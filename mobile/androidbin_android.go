//go:build android

package tsproxy

// gomobile embeds tsnet into a regular Android application with cgo enabled.
// The build-time patch enables androidbin's netmon fallback for that variant.
import _ "tailscale.com/feature/androidbin"
