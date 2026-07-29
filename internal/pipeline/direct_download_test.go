package pipeline

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestDownloadDirectAudioFetchesWholeMedia(t *testing.T) {
	original := directHTTPClient
	defer func() { directHTTPClient = original }()
	wav := oneSecondSilentWAV()
	directHTTPClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("Range"); got != "" {
				t.Fatalf("direct download unexpectedly used Range header %q", got)
			}
			finalURL, _ := url.Parse("https://1.1.1.1/final/audio.wav")
			return &http.Response{
				StatusCode: http.StatusOK, Status: "200 OK", ContentLength: int64(len(wav)),
				Header: http.Header{"Content-Type": []string{"audio/wav"}},
				Body:   io.NopCloser(bytes.NewReader(wav)), Request: &http.Request{URL: finalURL},
			}, nil
		})}
	}
	cfg := &Config{URL: "https://1.1.1.1/audio.wav", SourceType: "podcast", SourceID: "episode", Title: "Episode", MaxFileSize: "1M"}
	path, meta, err := Download(context.Background(), cfg, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer removeParent(path)
	downloaded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(downloaded, wav) {
		t.Fatalf("downloaded media differs: got %d bytes, want %d", len(downloaded), len(wav))
	}
	if meta.ID != "episode" || meta.Title != "Episode" || meta.Duration < 0.9 {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func TestParseByteSize(t *testing.T) {
	for input, want := range map[string]int64{"10M": 10 << 20, "2G": 2 << 30, "512": 512} {
		got, err := parseByteSize(input)
		if err != nil || got != want {
			t.Errorf("parseByteSize(%q)=(%d,%v), want %d", input, got, err, want)
		}
	}
}

func oneSecondSilentWAV() []byte {
	const sampleRate = 8000
	dataSize := uint32(sampleRate * 2)
	b := new(bytes.Buffer)
	b.WriteString("RIFF")
	_ = binary.Write(b, binary.LittleEndian, uint32(36)+dataSize)
	b.WriteString("WAVEfmt ")
	_ = binary.Write(b, binary.LittleEndian, uint32(16))
	_ = binary.Write(b, binary.LittleEndian, uint16(1))
	_ = binary.Write(b, binary.LittleEndian, uint16(1))
	_ = binary.Write(b, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(b, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(b, binary.LittleEndian, uint16(2))
	_ = binary.Write(b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(b, binary.LittleEndian, dataSize)
	b.Write(make([]byte, dataSize))
	return b.Bytes()
}

func removeParent(path string) { _ = os.RemoveAll(filepath.Dir(path)) }
