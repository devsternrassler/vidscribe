package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type Corpus struct {
	Schema     string     `json:"schema"`
	Thresholds Thresholds `json:"thresholds"`
	Cases      []Case     `json:"cases"`
}

type Thresholds struct {
	MaxMeanWER          float64 `json:"max_mean_wer"`
	MaxMeanCER          float64 `json:"max_mean_cer"`
	MinKeywordRecall    float64 `json:"min_keyword_recall"`
	MaxTimestampDriftMS float64 `json:"max_timestamp_drift_ms"`
}

type Case struct {
	ID                  string    `json:"id"`
	URL                 string    `json:"url,omitempty"`
	Language            string    `json:"language"`
	Category            string    `json:"category"`
	Reference           string    `json:"reference"`
	Keywords            []string  `json:"keywords"`
	ReferenceTimestamps []float64 `json:"reference_timestamps,omitempty"`
}

type CaseResult struct {
	ID               string   `json:"id"`
	WER              float64  `json:"wer"`
	CER              float64  `json:"cer"`
	KeywordRecall    float64  `json:"keyword_recall"`
	TimestampDriftMS *float64 `json:"timestamp_drift_ms,omitempty"`
}

type Report struct {
	Schema               string       `json:"schema"`
	Cases                []CaseResult `json:"cases"`
	MeanWER              float64      `json:"mean_wer"`
	MeanCER              float64      `json:"mean_cer"`
	KeywordRecall        float64      `json:"keyword_recall"`
	Passed               bool         `json:"passed"`
	Violations           []string     `json:"violations,omitempty"`
	MeanTimestampDriftMS *float64     `json:"mean_timestamp_drift_ms,omitempty"`
}

func EvaluateFile(corpusPath, hypothesesDir string) (*Report, error) {
	data, err := os.ReadFile(corpusPath)
	if err != nil {
		return nil, fmt.Errorf("read corpus: %w", err)
	}
	var corpus Corpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		return nil, fmt.Errorf("parse corpus: %w", err)
	}
	if corpus.Schema != "vidscribe-quality-corpus/v1" || len(corpus.Cases) == 0 {
		return nil, fmt.Errorf("invalid or empty quality corpus")
	}
	report := &Report{Schema: "vidscribe-quality-report/v1"}
	timestampCases, timestampTotal := 0, 0.0
	for _, testCase := range corpus.Cases {
		if testCase.ID == "" || testCase.Reference == "" {
			return nil, fmt.Errorf("case id and reference are required")
		}
		hypothesis, err := os.ReadFile(filepath.Join(hypothesesDir, testCase.ID+".txt"))
		if err != nil {
			return nil, fmt.Errorf("read hypothesis %s: %w", testCase.ID, err)
		}
		result := Evaluate(testCase.ID, testCase.Reference, string(hypothesis), testCase.Keywords)
		if len(testCase.ReferenceTimestamps) > 0 {
			value, driftErr := evaluateTimestampDrift(filepath.Join(hypothesesDir, testCase.ID+".json"), testCase.ReferenceTimestamps)
			if driftErr != nil {
				return nil, fmt.Errorf("timestamp drift %s: %w", testCase.ID, driftErr)
			}
			result.TimestampDriftMS = &value
			timestampCases++
			timestampTotal += value
		}
		report.Cases = append(report.Cases, result)
		report.MeanWER += result.WER
		report.MeanCER += result.CER
		report.KeywordRecall += result.KeywordRecall
	}
	n := float64(len(report.Cases))
	report.MeanWER /= n
	report.MeanCER /= n
	report.KeywordRecall /= n
	if timestampCases > 0 {
		value := timestampTotal / float64(timestampCases)
		report.MeanTimestampDriftMS = &value
	}
	report.Passed = true
	if corpus.Thresholds.MaxMeanWER > 0 && report.MeanWER > corpus.Thresholds.MaxMeanWER {
		report.Passed = false
		report.Violations = append(report.Violations, fmt.Sprintf("mean WER %.4f exceeds %.4f", report.MeanWER, corpus.Thresholds.MaxMeanWER))
	}
	if corpus.Thresholds.MaxMeanCER > 0 && report.MeanCER > corpus.Thresholds.MaxMeanCER {
		report.Passed = false
		report.Violations = append(report.Violations, fmt.Sprintf("mean CER %.4f exceeds %.4f", report.MeanCER, corpus.Thresholds.MaxMeanCER))
	}
	if report.KeywordRecall < corpus.Thresholds.MinKeywordRecall {
		report.Passed = false
		report.Violations = append(report.Violations, fmt.Sprintf("keyword recall %.4f below %.4f", report.KeywordRecall, corpus.Thresholds.MinKeywordRecall))
	}
	if corpus.Thresholds.MaxTimestampDriftMS > 0 && report.MeanTimestampDriftMS != nil && *report.MeanTimestampDriftMS > corpus.Thresholds.MaxTimestampDriftMS {
		report.Passed = false
		report.Violations = append(report.Violations, fmt.Sprintf("timestamp drift %.1fms exceeds %.1fms", *report.MeanTimestampDriftMS, corpus.Thresholds.MaxTimestampDriftMS))
	}
	return report, nil
}

func evaluateTimestampDrift(path string, reference []float64) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var doc struct {
		Segments []struct {
			Start float64 `json:"start"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return 0, err
	}
	if len(doc.Segments) != len(reference) {
		return 0, fmt.Errorf("segment count %d does not match reference %d", len(doc.Segments), len(reference))
	}
	total := 0.0
	for i, expected := range reference {
		delta := doc.Segments[i].Start - expected
		if delta < 0 {
			delta = -delta
		}
		total += delta * 1000
	}
	return total / float64(len(reference)), nil
}

func Evaluate(id, reference, hypothesis string, keywords []string) CaseResult {
	refWords, hypWords := words(reference), words(hypothesis)
	refRunes, hypRunes := []rune(strings.Join(refWords, " ")), []rune(strings.Join(hypWords, " "))
	wer, cer := ratio(distance(refWords, hypWords), len(refWords)), ratio(distanceRunes(refRunes, hypRunes), len(refRunes))
	recall := 1.0
	if len(keywords) > 0 {
		matched, normalizedHypothesis := 0, strings.Join(hypWords, " ")
		for _, keyword := range keywords {
			if strings.Contains(normalizedHypothesis, strings.Join(words(keyword), " ")) {
				matched++
			}
		}
		recall = float64(matched) / float64(len(keywords))
	}
	return CaseResult{ID: id, WER: wer, CER: cer, KeywordRecall: recall}
}

func words(value string) []string {
	return strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSpace(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, value))
}

func distance(a, b []string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func distanceRunes(a, b []rune) int {
	sa, sb := make([]string, len(a)), make([]string, len(b))
	for i, r := range a {
		sa[i] = string(r)
	}
	for i, r := range b {
		sb[i] = string(r)
	}
	return distance(sa, sb)
}
func ratio(value, total int) float64 {
	if total == 0 {
		if value == 0 {
			return 0
		}
		return 1
	}
	return float64(value) / float64(total)
}
