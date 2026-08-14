package traefik_throttle_test

import (
	_ "embed"
	"testing"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

//go:embed main.go
var pluginSource string

// Traefik loads plugins by interpreting their source with Yaegi, which supports
// only a subset of Go. This guards against changes that compile natively but
// would fail to load inside Traefik.
func TestYaegiCanInterpretPlugin(t *testing.T) {
	i := interp.New(interp.Options{})
	if err := i.Use(stdlib.Symbols); err != nil {
		t.Fatalf("use stdlib: %v", err)
	}
	if _, err := i.Eval(pluginSource); err != nil {
		t.Fatalf("plugin is not Yaegi-compatible: %v", err)
	}

	// The exported entry points Traefik calls must be reachable and usable.
	if _, err := i.Eval("traefik_throttle.CreateConfig()"); err != nil {
		t.Fatalf("CreateConfig not usable under Yaegi: %v", err)
	}
}
