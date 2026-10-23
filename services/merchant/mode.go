package merchant

import "fmt"

// Mode describes merchant integration mode shared across internal merchant-service contracts.
type Mode uint8

const (
	ModeDemo Mode = iota + 1
	ModeLive
)

var modeToString = map[Mode]string{
	ModeDemo: "demo",
	ModeLive: "live",
}

// NewMode creates Mode from uint8 value.
func NewMode(value uint8) Mode {
	return Mode(value)
}

// NewModeFromCode creates Mode from gRPC/service numeric code.
func NewModeFromCode(value int32) (Mode, error) {
	switch value {
	case int32(ModeDemo):
		return ModeDemo, nil
	case int32(ModeLive):
		return ModeLive, nil
	default:
		return 0, fmt.Errorf("[merchant][NewModeFromCode][err: unsupported mode %d]", value)
	}
}

// String returns canonical string representation of Mode.
func (m Mode) String() string {
	if value, ok := modeToString[m]; ok {
		return value
	}
	return "unknown"
}

// Uint8 converts Mode to uint8.
func (m Mode) Uint8() uint8 {
	return uint8(m)
}

// Int32 converts Mode to int32 for gRPC transport compatibility.
func (m Mode) Int32() int32 {
	return int32(m)
}

// IsValid reports whether Mode is supported.
func (m Mode) IsValid() bool {
	_, ok := modeToString[m]
	return ok
}
