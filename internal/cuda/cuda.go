package cuda

import (
	"runtime"

	"github.com/sternrassler/vidscribe/internal/runtimeenv"
)

// UvxCublasFlag is the pip package name injected via "uvx --with" to provide libcublas.so.12.
const UvxCublasFlag = runtimeenv.NvidiaCublas

// NeedsBundledCublas reports whether the Linux-only wheel injection is valid.
// Windows CUDA uses the native DLL search path; macOS has no CUDA wheel.
func NeedsBundledCublas(device string) bool {
	return device == "cuda" && runtime.GOOS == "linux"
}

// cublasLDSetup is the shared Python preamble that locates nvidia-cublas-cu12's lib
// directory and prepends it to LD_LIBRARY_PATH.
const cublasLDSetup = `import nvidia.cublas, pathlib, os
lib_dir = str(pathlib.Path(nvidia.cublas.__spec__.submodule_search_locations[0]) / "lib")
env = os.environ.copy()
env["LD_LIBRARY_PATH"] = lib_dir + ":" + env.get("LD_LIBRARY_PATH", "")
`

// CheckScript is a Python script that verifies libcublas.so.12 is loadable
// via the nvidia-cublas-cu12 package. Prints "ok" on success, exits 1 on failure.
const CheckScript = cublasLDSetup + `import ctypes, sys
lib_path = lib_dir + "/libcublas.so.12"
try:
    ctypes.CDLL(lib_path)
    print("ok")
except Exception as e:
    print("fail: " + str(e))
    sys.exit(1)
`

// WhisperWrapperScript is a Python script that launches whisper-ctranslate2
// with LD_LIBRARY_PATH set so ctranslate2 can find libcublas.so.12.
const WhisperWrapperScript = cublasLDSetup + `import subprocess, sys
sys.exit(subprocess.call(["whisper-ctranslate2"] + sys.argv[1:], env=env))
`
