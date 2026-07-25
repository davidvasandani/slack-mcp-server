package handler

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
)

var fileIDRegex = regexp.MustCompile(`^F[A-Z0-9]+$`)

var errDownloadSizeExceeded = errors.New("download exceeded size limit")

type limitedWriter struct {
	buf     *bytes.Buffer
	written int
	limit   int
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if len(p) > lw.limit-lw.written {
		return 0, errDownloadSizeExceeded
	}
	n, err := lw.buf.Write(p)
	lw.written += n
	return n, err
}

func mapSlackFileError(err error, fileID string) error {
	if err == nil {
		return nil
	}

	switch err.Error() {
	case "missing_scope":
		return errors.New("Slack API error: the connected identity is missing a required file-read OAuth scope; update the Slack app OAuth scopes and reinstall the app")
	case "not_authed", "invalid_auth":
		return errors.New("Slack API error: connector credentials are invalid or missing; reconnect Slack and retry")
	case "access_denied":
		return errors.New("Slack API error: the file is not accessible to the connected identity; verify channel membership and file permissions")
	case "file_not_found":
		return fmt.Errorf("Slack API error: file %s does not exist or is not accessible", fileID)
	case "file_deleted":
		return fmt.Errorf("Slack API error: file %s has been deleted", fileID)
	default:
		return errors.New("Slack API error while retrieving file metadata; check the Slack app configuration or retry")
	}
}

func sanitizeDownloadError(err error) string {
	type httpStatusCoder interface {
		HTTPStatusCode() int
	}

	var statusErr httpStatusCoder
	if errors.As(err, &statusErr) {
		return fmt.Sprintf("Slack download returned HTTP status %d", statusErr.HTTPStatusCode())
	}

	return "download failed"
}
