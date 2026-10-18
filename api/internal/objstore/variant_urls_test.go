package objstore

// t_466ff771: the media listing derives variant URLs from the base key, so a
// derived key is not evidence that an object was written. These tests pin the
// rule VariantURLs / PublicURLIfPresent exist to enforce: a URL is advertised
// ONLY when the store confirms the object is there, and a failed HEAD (could not
// tell) is treated as absent rather than as a green light.

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// variantProbe is an ObjectExister that answers from a key set and records what
// it was asked about.
type variantProbe struct {
	present map[string]bool
	errOn   map[string]error
	asked   []string
}

func newVariantProbe(keys ...string) *variantProbe {
	p := &variantProbe{present: map[string]bool{}, errOn: map[string]error{}}
	for _, k := range keys {
		p.present[k] = true
	}
	return p
}

func (p *variantProbe) ObjectExists(_ context.Context, key string) (bool, error) {
	p.asked = append(p.asked, key)
	if err, ok := p.errOn[key]; ok {
		return false, err
	}
	return p.present[key], nil
}

func testPublicURL(key string) string { return "https://media.test/" + key }

func TestVariantURLsOnlyAdvertisesObjectsThatExist(t *testing.T) {
	const base = "u/acct-1/abc.jpg"
	thumb := BuildVariantKey(base, SizeThumb)
	medium := BuildVariantKey(base, SizeMedium)
	full := BuildVariantKey(base, SizeFull)
	for _, k := range []string{thumb, medium, full} {
		if k == "" {
			t.Fatalf("fixture: BuildVariantKey(%q) is empty", base)
		}
	}

	t.Run("all three written", func(t *testing.T) {
		p := newVariantProbe(thumb, medium, full)
		got := VariantURLs(context.Background(), base, p, testPublicURL)
		if len(got) != len(ImageSizes) {
			t.Fatalf("VariantURLs = %v, want all %d variants", got, len(ImageSizes))
		}
		for _, size := range ImageSizes {
			if want := testPublicURL(BuildVariantKey(base, size)); got[string(size)] != want {
				t.Errorf("size %q = %q, want %q", size, got[string(size)], want)
			}
		}
		for _, k := range p.asked {
			if k == base {
				t.Errorf("the base object was probed; only derived variant keys need a HEAD")
			}
		}
	})

	t.Run("none written (an avatar row)", func(t *testing.T) {
		// uploadAvatar stores one artifact and writes no variants. The base
		// object being present must not make the listing invent three siblings.
		p := newVariantProbe(base)
		if got := VariantURLs(context.Background(), base, p, testPublicURL); got != nil {
			t.Fatalf("VariantURLs on a row with no variants = %v, want nil", got)
		}
		if len(p.asked) != len(ImageSizes) {
			t.Errorf("probed %d keys (%v), want one HEAD per variant", len(p.asked), p.asked)
		}
	})

	t.Run("partial write", func(t *testing.T) {
		// The upload path only WARNS when a variant Put fails, so a row can
		// legitimately have a subset. Advertise exactly what is there.
		p := newVariantProbe(thumb)
		got := VariantURLs(context.Background(), base, p, testPublicURL)
		if len(got) != 1 || got[string(SizeThumb)] == "" {
			t.Fatalf("VariantURLs = %v, want only the thumbnail", got)
		}
		if _, ok := got[string(SizeFull)]; ok {
			t.Errorf("VariantURLs advertised a full variant that was never written: %v", got)
		}
	})

	t.Run("head error fails closed", func(t *testing.T) {
		p := newVariantProbe(thumb, medium, full)
		p.errOn[medium] = errors.New("s3 head: connection reset")
		got := VariantURLs(context.Background(), base, p, testPublicURL)
		if _, ok := got[string(SizeMedium)]; ok {
			t.Errorf("VariantURLs advertised a variant the store could not confirm: %v", got)
		}
		if len(got) != 2 {
			t.Errorf("VariantURLs = %v, want the two confirmed variants", got)
		}
	})

	t.Run("non-canonical key", func(t *testing.T) {
		p := newVariantProbe("etc/passwd-thumbnail.jpg")
		if got := VariantURLs(context.Background(), "etc/passwd", p, testPublicURL); got != nil {
			t.Fatalf("VariantURLs on a non-upload key = %v, want nil", got)
		}
	})

	t.Run("no store", func(t *testing.T) {
		if got := VariantURLs(context.Background(), base, nil, testPublicURL); got != nil {
			t.Fatalf("VariantURLs with no store = %v, want nil", got)
		}
	})
}

func TestPublicURLIfPresentOnlyAdvertisesExistingObjects(t *testing.T) {
	const key = "u/acct-1/abc-preview.jpg"

	if got := PublicURLIfPresent(context.Background(), key, newVariantProbe(key), testPublicURL); got != testPublicURL(key) {
		t.Fatalf("PublicURLIfPresent(present) = %q, want %q", got, testPublicURL(key))
	}
	if got := PublicURLIfPresent(context.Background(), key, newVariantProbe(), testPublicURL); got != "" {
		t.Fatalf("PublicURLIfPresent(absent) = %q, want \"\"", got)
	}
	errProbe := newVariantProbe(key)
	errProbe.errOn[key] = errors.New("s3 head: timeout")
	if got := PublicURLIfPresent(context.Background(), key, errProbe, testPublicURL); got != "" {
		t.Fatalf("PublicURLIfPresent(head error) = %q, want \"\" (never advertise what was not confirmed)", got)
	}
	if got := PublicURLIfPresent(context.Background(), "", newVariantProbe(key), testPublicURL); got != "" {
		t.Fatalf("PublicURLIfPresent(empty key) = %q, want \"\"", got)
	}
	if got := PublicURLIfPresent(context.Background(), key, nil, testPublicURL); got != "" {
		t.Fatalf("PublicURLIfPresent(nil store) = %q, want \"\"", got)
	}
	if got := PublicURLIfPresent(context.Background(), key, newVariantProbe(key), nil); got != "" {
		t.Fatalf("PublicURLIfPresent(nil publicURL) = %q, want \"\"", got)
	}
}

// TestVariantURLsShapeOfDerivedKeys: the URLs a lister advertises are the
// sibling keys the upload pipeline writes, not some other namespace.
func TestVariantURLsShapeOfDerivedKeys(t *testing.T) {
	const base = "u/acct-1/abc.jpg"
	p := newVariantProbe(
		"u/acct-1/abc-thumbnail.jpg",
		"u/acct-1/abc-medium.jpg",
		"u/acct-1/abc-full.jpg",
	)
	got := VariantURLs(context.Background(), base, p, testPublicURL)
	for size, url := range got {
		if !strings.HasSuffix(url, "-"+size+".jpg") {
			t.Errorf("size %q url %q does not name the %q variant of %q", size, url, size, base)
		}
	}
}
