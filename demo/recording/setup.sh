# Sourced, hidden, by record.tape. Builds the CLI and copies the checkout
# assessment fixture plus one weak-wait spec into a throwaway project.
root=$(git rev-parse --show-toplevel)
demo=/tmp/9l-demo
rm -rf "$demo" && mkdir -p "$demo"
go build -C "$root" -o "$demo/bin/9l" ./cmd/9l
cp -R "$root/testdata/assessment/." "$demo/"
cp "$root/demo/recording/dialog.spec.ts" "$demo/tests/"
ln -s "$root/node_modules" "$demo/node_modules"
export PATH="$demo/bin:$PATH" PS1='$ '
cd "$demo" || return

# Per-test status from the last run's validated Playwright evidence.
last-run-tests() {
  local run
  run=$(ls -t .9lives/receipts | grep '^run-' | head -1)
  jq -r '.. | .specs? // empty | .[] | .tests[0].results[0] as $r
    | "\($r.status)\t\(.title)" + (if $r.status == "failed"
      then "  ← line \($r.errors[0].location.line)" else "" end)' \
    .9lives/receipts/"$run"/job-001/*/stdout.log
}
clear
