package walk

import (
	"errors"
	"syscall"

	"disk-report/internal/schema"
)

// errnoNames covers the codes a filesystem walk actually produces. The names
// are not cosmetic: the SPA keys its "grant Full Disk Access" banner off EPERM
// and EACCES exactly, so an unrecognised code would silently drop the warning
// that explains a missing ~/Library/Mail.
var errnoNames = map[syscall.Errno]string{
	syscall.EPERM:        "EPERM",
	syscall.EACCES:       "EACCES",
	syscall.ENOENT:       "ENOENT",
	syscall.ELOOP:        "ELOOP",
	syscall.ENOTDIR:      "ENOTDIR",
	syscall.EIO:          "EIO",
	syscall.ENAMETOOLONG: "ENAMETOOLONG",
	syscall.EMFILE:       "EMFILE",
	syscall.ENFILE:       "ENFILE",
	syscall.ENODEV:       "ENODEV",
	syscall.ESTALE:       "ESTALE",
	syscall.ETIMEDOUT:    "ETIMEDOUT",
	syscall.EBADF:        "EBADF",
	syscall.ENOTSUP:      "ENOTSUP",
}

// ToScanError records a failed read as data.
//
// Permission errors are not swallowed: without Full Disk Access macOS hides
// ~/Library/Mail, ~/Library/Messages and the Photos library, and omitting them
// silently would under-count by tens of gigabytes with no visible sign.
func ToScanError(path string, err error) schema.ScanError {
	code := "UNKNOWN"

	var errno syscall.Errno
	if errors.As(err, &errno) {
		if name, ok := errnoNames[errno]; ok {
			code = name
		}
	}

	return schema.ScanError{Path: path, Code: code, Message: err.Error()}
}
