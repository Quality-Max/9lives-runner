//go:build windows

package runner

import "os"

// pipeHeldEmpty never claims a Windows pipe is held empty. os.Pipe creates a
// synchronous anonymous pipe, and PeekNamedPipe on its read handle waits
// behind the copy's pending ReadFile, so inspecting it during the drain would
// block forever. It is also not needed here: every owned process runs in a
// job object without JOB_OBJECT_LIMIT_BREAKAWAY_OK, so no child can leave it,
// and the job is stopped before the drain, after which every write end is
// closed and each pipe reaches end of file. The drain therefore keeps reading
// until then, with the byte and time bounds as the backstop.
func pipeHeldEmpty(*os.File) (bool, error) {
	return false, nil
}
