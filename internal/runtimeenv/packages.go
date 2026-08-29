package runtimeenv

// Runtime package pins are updated deliberately after the integration matrix
// passes. Keeping them here makes every CLI, MCP and dependency probe use the
// same tested toolchain.
const (
	YTDLP              = "yt-dlp[default,curl-cffi]@2026.7.4"
	WhisperCTranslate2 = "whisper-ctranslate2==0.5.7"
	OpenAIWhisper      = "openai-whisper==20250625"
	ONNXASR            = "onnx-asr[cpu,hub]==0.12.0"
	NvidiaCublas       = "nvidia-cublas-cu12==12.9.2.10"
	ParakeetModel      = "nemo-parakeet-tdt-0.6b-v3"
)

func Versions() map[string]string {
	return map[string]string{
		"yt-dlp":              YTDLP,
		"whisper-ctranslate2": WhisperCTranslate2,
		"openai-whisper":      OpenAIWhisper,
		"onnx-asr":            ONNXASR,
		"nvidia-cublas-cu12":  NvidiaCublas,
		"parakeet-model":      ParakeetModel,
	}
}
