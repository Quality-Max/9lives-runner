package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestFlagsFirstAllowsFlagsAfterSpecs(t *testing.T) {
	got := flagsFirst([]string{"a.spec.ts", "--workers", "3", "b.spec.ts", "--format=json"}, map[string]bool{"--workers": true, "--format": true})
	want := []string{"--workers", "3", "--format=json", "a.spec.ts", "b.spec.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestRunRejectsMissingSpecs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"plan"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestPreviewDiffDoesNotUseJSONWriter(t *testing.T) {
	var preview, result bytes.Buffer
	previewDiff(&preview)("line\n", "changed\n")
	if preview.Len() == 0 || result.Len() != 0 || !bytes.Contains(preview.Bytes(), []byte("+++ verified candidate")) {
		t.Fatalf("preview=%q result=%q", preview.String(), result.String())
	}
}

func TestNativeRunTimeoutReadsPositiveEnvironmentOverride(t *testing.T) {
	t.Setenv("NINELIVES_RUN_TIMEOUT", "17")
	if got := nativeRunTimeout(); got.String() != "17s" {
		t.Fatalf("timeout=%s", got)
	}
	t.Setenv("NINELIVES_RUN_TIMEOUT", "not-a-duration")
	if got := nativeRunTimeout(); got.String() != "5m0s" {
		t.Fatalf("fallback=%s", got)
	}
}

type nonInterruptibleReader struct{ release <-chan struct{} }

func (r nonInterruptibleReader) Read([]byte) (int, error) {
	<-r.release
	return 0, io.EOF
}

func TestReadTerminalApprovalSelectsCancellationWhenReaderCannotBeClosed(t *testing.T) {
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan bool, 1)
	go func() { result <- readTerminalApproval(ctx, nonInterruptibleReader{release: release}, io.Discard) }()
	cancel()
	select {
	case approved := <-result:
		if approved {
			t.Fatal("canceled approval accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled approval waited for non-interruptible reader")
	}
	close(release)
}

func TestReadTerminalApprovalCancelsBlockedRead(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan bool, 1)
	go func() { result <- readTerminalApproval(ctx, reader, io.Discard) }()
	cancel()
	select {
	case approved := <-result:
		if approved {
			t.Fatal("canceled approval accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled approval remained blocked")
	}
}

func TestReadTerminalApprovalAcceptsOnlyYOrYes(t *testing.T) {
	for input, want := range map[string]bool{"y\n": true, "yes\n": true, "n\n": false} {
		t.Run(input, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(writer, input); err != nil {
				t.Fatal(err)
			}
			_ = writer.Close()
			if got := readTerminalApproval(context.Background(), reader, io.Discard); got != want {
				t.Fatalf("got=%v want=%v", got, want)
			}
		})
	}
}
