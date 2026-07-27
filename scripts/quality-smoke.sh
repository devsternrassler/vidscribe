#!/usr/bin/env bash
set -euo pipefail

log_file="${HOME}/.vidscribe-quality-smoke.log"
tmp_dir="$(mktemp -d /tmp/vidscribe-quality-XXXXXX)"
trap 'rm -rf -- "${tmp_dir}"' EXIT
exec > >(tee -a "${log_file}") 2>&1

go run . "https://www.youtube.com/watch?v=jNQXAC9IVRw" \
  --profile custom --engine faster --model large-v3 --device cpu \
  --compute-type int8 --language en --format txt --output-dir "${tmp_dir}/out"

transcript="$(find "${tmp_dir}/out" -maxdepth 1 -type f -name '*.txt' -print -quit)"
if [[ -z "${transcript}" ]]; then
  echo "quality smoke: transcript missing" >&2
  exit 1
fi
mkdir -p "${tmp_dir}/hypotheses"
cp "${transcript}" "${tmp_dir}/hypotheses/me-at-the-zoo-en-short.txt"
go run . quality-eval --corpus quality/corpus.json --hypotheses "${tmp_dir}/hypotheses"
