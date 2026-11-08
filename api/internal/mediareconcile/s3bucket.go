package mediareconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Bucket is the object-store surface the reconcile needs. Keeping it an
// interface lets the classification and apply rules be tested without S3.
type Bucket interface {
	// List returns every object under prefix, paginated internally.
	List(ctx context.Context, prefix string) ([]Object, error)
	// Delete removes the given keys. It must be all-or-nothing from the caller's
	// point of view: every key is verified gone afterwards by the command.
	Delete(ctx context.Context, keys []string) error
	// Exists reports whether a key is still present.
	Exists(ctx context.Context, key string) (bool, error)
}

// S3Bucket is a Bucket backed by S3 or MinIO.
type S3Bucket struct {
	client *s3.Client
	bucket string
}

// NewS3Bucket builds the client. Path-style addressing is enabled for EVERY
// custom endpoint, exactly as objstore.NewObjectStore does, so the reconcile
// tool reaches the same store the API does.
func NewS3Bucket(endpoint, accessKey, secretKey, bucket, region string) (*S3Bucket, error) {
	if bucket == "" {
		return nil, errors.New("bucket empty")
	}
	if region == "" {
		region = "us-east-1"
	}
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if accessKey != "" && secretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")))
	}
	if endpoint != "" {
		opts = append(opts, awsconfig.WithBaseEndpoint(endpoint))
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("s3 config: %w", err)
	}
	cli := s3.NewFromConfig(cfg, func(o *s3.Options) {
		// PATH-STYLE FOR EVERY CUSTOM ENDPOINT, not just the four substrings this
		// used to sniff for (`localhost`, `127.0.0.1`, `minio`, `:9000`).
		//
		// A custom endpoint means "not AWS S3": it is a self-hosted or
		// third-party S3-compatible server (MinIO, SeaweedFS, Garage, Ceph RGW,
		// R2, Spaces …). Virtual-host addressing asks the resolver for
		// `<bucket>.<endpoint>`, which only exists if that service publishes a
		// wildcard DNS record — compose-network service names never do. The old
		// sniff made the tool work ONLY against a service named `minio` on
		// :9000, so the production switch to `S3_ENDPOINT=http://s3:8333`
		// (SeaweedFS) built `jomform-media.s3:8333` and died on DNS — an ops
		// tool that could no longer read the store exactly when it was needed.
		// With no endpoint set we are on real AWS S3, where virtual-host style
		// is correct and path-style is being retired — so the rule is
		// "endpoint set ⇒ path".
		if endpoint != "" {
			o.UsePathStyle = true
		}
	})
	return &S3Bucket{client: cli, bucket: bucket}, nil
}

// List pages through the whole prefix. A partial listing would understate the
// account's bytes and could authorise a wrong quota, so a mid-pagination error is
// returned rather than a short slice.
func (b *S3Bucket) List(ctx context.Context, prefix string) ([]Object, error) {
	p := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: &b.bucket,
		Prefix: &prefix,
	})
	var out []Object
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list %q: %w", prefix, err)
		}
		for _, o := range page.Contents {
			obj := Object{Key: str(o.Key), Size: deref64(o.Size)}
			if o.LastModified != nil {
				obj.LastModified = *o.LastModified
			}
			obj.ETag = strings.Trim(str(o.ETag), `"`)
			out = append(out, obj)
		}
	}
	return out, nil
}

// Delete removes keys in batches of 1000 (the S3 DeleteObjects limit). Any error
// in any batch aborts, and the returned error names the keys so the operator can
// see what did and did not go.
func (b *S3Bucket) Delete(ctx context.Context, keys []string) error {
	const batch = 1000
	for start := 0; start < len(keys); start += batch {
		end := start + batch
		if end > len(keys) {
			end = len(keys)
		}
		ids := make([]types.ObjectIdentifier, 0, end-start)
		for _, k := range keys[start:end] {
			ids = append(ids, types.ObjectIdentifier{Key: strptr(k)})
		}
		resp, err := b.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: &b.bucket,
			Delete: &types.Delete{Objects: ids, Quiet: boolptr(true)},
		})
		if err != nil {
			return fmt.Errorf("s3 delete objects: %w", err)
		}
		if len(resp.Errors) > 0 {
			e := resp.Errors[0]
			return fmt.Errorf("s3 delete objects: %s: %s", str(e.Key), str(e.Message))
		}
	}
	return nil
}

// Exists is a HEAD probe.
func (b *S3Bucket) Exists(ctx context.Context, key string) (bool, error) {
	_, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &b.bucket, Key: &key})
	if err == nil {
		return true, nil
	}
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return false, nil
	}
	// MinIO answers a missing HEAD with a generic 404 that the SDK does not
	// always map onto types.NotFound, so the status code is the fallback.
	var re interface{ HTTPStatusCode() int }
	if errors.As(err, &re) && re.HTTPStatusCode() == 404 {
		return false, nil
	}
	return false, fmt.Errorf("s3 head %s: %w", key, err)
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func strptr(s string) *string { return &s }

func boolptr(b bool) *bool { return &b }

func deref64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
