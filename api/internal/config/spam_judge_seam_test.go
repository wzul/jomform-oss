package config

// The spam judge's own seam (card t_0e9dc7fc): SPAM_JUDGE_MODEL and
// SPAM_JUDGE_THRESHOLD are read independently of LLM_MODEL, because the judge
// asks the provider's decision route (/v1/systemone) with a different model.
// A parse failure on the threshold must fall back to the default rather than
// silently meaning "flag nothing" (a negative float) or "flag everything"
// (a typo read as 0).

import (
	"testing"

	"github.com/wzul/jomform/api/internal/llm"
)

// loadForSeam loads a config with the minimum env a deployment needs.
func loadForSeam(t *testing.T) *Config {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "dev")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestSpamJudgeSeamDefaults(t *testing.T) {
	t.Setenv("SPAM_JUDGE_MODEL", "")
	t.Setenv("SPAM_JUDGE_THRESHOLD", "")
	cfg := loadForSeam(t)
	if cfg.SpamJudgeModel != llm.DefaultSpamJudgeModel {
		t.Errorf("SpamJudgeModel = %q, want the decision default %q",
			cfg.SpamJudgeModel, llm.DefaultSpamJudgeModel)
	}
	if cfg.SpamJudgeThreshold != llm.DefaultSpamJudgeThreshold {
		t.Errorf("SpamJudgeThreshold = %v, want the default %v",
			cfg.SpamJudgeThreshold, llm.DefaultSpamJudgeThreshold)
	}
}

func TestSpamJudgeSeamOverrides(t *testing.T) {
	t.Setenv("SPAM_JUDGE_MODEL", "another-decision-model")
	t.Setenv("SPAM_JUDGE_THRESHOLD", "0.8")
	cfg := loadForSeam(t)
	if cfg.SpamJudgeModel != "another-decision-model" {
		t.Errorf("SpamJudgeModel = %q, want the configured model", cfg.SpamJudgeModel)
	}
	if cfg.SpamJudgeThreshold != 0.8 {
		t.Errorf("SpamJudgeThreshold = %v, want 0.8", cfg.SpamJudgeThreshold)
	}
}

func TestSpamJudgeThresholdFallsBackOnBadInput(t *testing.T) {
	// Each of these is a typo, not a policy: "abc" and "NaN" do not parse, and
	// 1.5 / -0.1 are outside the 0..1 confidence range. All must land on the
	// default rather than on a value that would silently disable or blanket the
	// judge.
	for _, raw := range []string{"abc", "NaN", "1.5", "-0.1", " "} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("SPAM_JUDGE_THRESHOLD", raw)
			cfg := loadForSeam(t)
			if cfg.SpamJudgeThreshold != llm.DefaultSpamJudgeThreshold {
				t.Fatalf("SPAM_JUDGE_THRESHOLD=%q gave %v, want the default %v",
					raw, cfg.SpamJudgeThreshold, llm.DefaultSpamJudgeThreshold)
			}
		})
	}
}

func TestSpamJudgeThresholdAcceptsZero(t *testing.T) {
	// 0 is a real policy ("flag everything"), so it must survive the parse — the
	// fallback is for typos, not for aggressive settings.
	t.Setenv("SPAM_JUDGE_THRESHOLD", "0")
	cfg := loadForSeam(t)
	if cfg.SpamJudgeThreshold != 0 {
		t.Fatalf("SPAM_JUDGE_THRESHOLD=0 gave %v, want 0", cfg.SpamJudgeThreshold)
	}
}
