package spamjudge

// Card t_28baedff — LIVE calibration over Malay fixtures.
//
// This is the opt-in half of the guard, mirroring
// llm.TestSpamJudgeModelIsPublishedByTheRouter: the gate stays hermetic (no
// network, no key) and this test runs only when explicitly enabled, when the
// router or the judge prompt changes.
//
//   JOMFORM_LIVE_SPAM_CALIBRATION=1 \
//   LLM_BASE_URL=https://router.example.com/v1 \
//   OLLAMA_API_KEY=... go test ./internal/spamjudge/ -run TestLiveSpamJudgeCalibration -v
//
// WHY IT EXISTS: the unit tests prove the judged state carries only the
// sender's answers; they cannot prove the decision MODEL reads a genuine Malay
// enquiry as genuine. The default threshold sits at 0.5 and a real prod
// submission cleared it by 0.017, so "the verdict is right on my sample" is not
// enough — this test asserts a MARGIN, and a genuine sample that creeps toward
// the line fails before it is ever flagged in production.
//
// The decimals are deliberately NOT pinned (the answer is prompt-sensitive and
// a model upgrade may move it); what is pinned is: genuine < threshold - margin,
// spam >= threshold + margin, and a coarse bound on the BM/EN gap.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/llm"
)

// calibrationMargin is how far a sample must sit from the default threshold for
// the run to count as calibrated. 0.10 is wide enough to absorb the drift we
// measured across runs of one prompt (none: identical prompt, identical noul)
// and narrow enough that a model change which halves the margin fails.
const calibrationMargin = 0.10

func TestLiveSpamJudgeCalibration(t *testing.T) {
	if os.Getenv("JOMFORM_LIVE_SPAM_CALIBRATION") != "1" {
		t.Skip("live calibration: set JOMFORM_LIVE_SPAM_CALIBRATION=1 with LLM_BASE_URL/OLLAMA_API_KEY " +
			"to re-check that genuine Malay submissions (including one offering a phone number) clear " +
			"the default spam threshold with margin")
	}
	base := strings.TrimRight(os.Getenv("LLM_BASE_URL"), "/")
	if base == "" {
		t.Fatal("LLM_BASE_URL is unset: the calibration has no router to ask")
	}
	if os.Getenv("OLLAMA_API_KEY") == "" {
		t.Fatal("OLLAMA_API_KEY is unset: the router will answer 401 and every sample fails open")
	}
	client := llm.New(base, "").SetSpamJudge(llm.DefaultSpamJudgeModel, llm.DefaultSpamJudgeThreshold)

	score := func(name, text string) float64 {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		// A real submit: the renderer's hidden anti-spam controls ride the same
		// fields map, which is exactly the framing that produced the prod false
		// positive. If the judge regresses and feeds them again, this sample is
		// what moves.
		data := map[string]any{
			"b1":      text,
			"jf_ts":   "1793037169",
			"website": "",
		}
		noul, err := client.SpamScore(ctx, buildPrompt(Job{FormTitle: "cuba", Data: data}))
		if err != nil {
			t.Fatalf("%s: decision call failed: %v", name, err)
		}
		t.Logf("%-38s noul=%.4f (threshold %.2f)", name, noul, llm.DefaultSpamJudgeThreshold)
		return noul
	}

	genuine := []struct{ name, text string }{
		{"BM enquiry (the prod false positive)", genuineBMMessage},
		{"BM enquiry (scheduling)", "Assalamualaikum, saya nak tanya pasal harga kelas mengaji untuk anak perempuan saya umur 10 tahun. Boleh tak bagi saya jadual kelas hujung minggu?"},
		{"BM registration question", "Salam, saya berminat untuk mendaftar anak saya ke kelas Al-Quran. Berapa yuran sebulan dan di mana lokasi kelas ya?"},
		{"BM with a phone number for a reply", "Saya ingin bertanya tentang kelas mengaji untuk anak saya yang berumur 8 tahun. Boleh hubungi saya di 0123456789 untuk maklumat lanjut."},
		{"BM with a WhatsApp number", "Saya berminat untuk mendaftar anak saya ke kelas Al-Quran. Boleh WhatsApp saya di 0123456789 untuk maklumat lanjut."},
		{"English with a phone number", "I would like to ask about the classes for my son who is 8 years old. You can reach me on 0123456789 for more information."},
	}
	for _, g := range genuine {
		noul := score(g.name, g.text)
		if noul >= llm.DefaultSpamJudgeThreshold {
			t.Errorf("genuine submission %q scored %.4f: at or above the default threshold %.2f — it would be "+
				"flagged, and the retention sweep deletes an unreviewed flag", g.name, noul, llm.DefaultSpamJudgeThreshold)
			continue
		}
		if noul > llm.DefaultSpamJudgeThreshold-calibrationMargin {
			t.Errorf("genuine submission %q scored %.4f: below the threshold but inside the %.2f margin — "+
				"recalibrate the judge prompt before it starts dropping real enquiries", g.name, noul, calibrationMargin)
		}
	}

	spam := []struct{ name, text string }{
		{"backlink advert", "Buy cheap backlinks now!! 10000 backlinks for $5, free crypto giveaway, click http://spam.example/win to claim your prize immediately."},
		{"link-only advert", "Click here to claim your prize: http://spam.example/win"},
		{"crypto giveaway", "FREE CRYPTO GIVEAWAY!!! Send 0.1 ETH to 0xabc123 and receive 1 ETH back instantly."},
	}
	for _, s := range spam {
		noul := score(s.name, s.text)
		if noul < llm.DefaultSpamJudgeThreshold+calibrationMargin {
			t.Errorf("spam submission %q scored %.4f: an obvious advert must clear the threshold %.2f by at "+
				"least %.2f, or the criteria have been loosened into uselessness", s.name, noul, llm.DefaultSpamJudgeThreshold, calibrationMargin)
		}
	}

	// Item 3 of the card: the same message must not read as ~0.4 more spammy in
	// Malay than in English. The bound is coarse on purpose — the language gap
	// that survived a prompt fix measured ~0.19 on the raw messages and ~0.37 in
	// the framing that carried the anti-spam fields, so 0.25 is a real regression
	// signal rather than a fit to today's decimals.
	bmGapSample := "Saya ingin bertanya tentang kelas mengaji untuk anak saya yang berumur 8 tahun. Boleh hubungi saya pada waktu pejabat."
	enGapSample := "I would like to ask about the Quran recital classes for my son who is 8 years old. You can contact me during office hours."
	gap := score("gap: BM", bmGapSample) - score("gap: English", enGapSample)
	if gap > 0.25 {
		t.Errorf("the same message scores %.4f higher in Malay than in English — the judge is not "+
			"language-neutral, and a Malay-speaking store pays for it in false positives", gap)
	}
}
