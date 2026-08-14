//go:build !darwin && !linux

package protocol

// Snow's runtime and public SDK require macOS or Linux.
var _ = Snow_requires_macOS_or_Linux
