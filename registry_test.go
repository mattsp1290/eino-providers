package einoproviders

import (
	"context"
	"testing"
)

func TestRegisterProviderAndLookup(t *testing.T) {
	name := t.Name()
	ctor := func(context.Context, string, Options) (Provider, error) {
		return nil, nil
	}

	RegisterProvider(name, ctor)

	got, ok := lookupProvider(name)
	if !ok {
		t.Fatalf("lookupProvider(%q) ok = false, want true", name)
	}
	if got == nil {
		t.Fatal("lookupProvider returned nil constructor")
	}
}

func TestRegisterProviderDuplicatePanics(t *testing.T) {
	name := t.Name()
	ctor := func(context.Context, string, Options) (Provider, error) {
		return nil, nil
	}

	RegisterProvider(name, ctor)

	defer func() {
		if recover() == nil {
			t.Fatal("RegisterProvider duplicate did not panic")
		}
	}()
	RegisterProvider(name, ctor)
}

func TestRegisterProviderRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		call func()
	}{
		{
			name: "empty name",
			call: func() {
				RegisterProvider("", func(context.Context, string, Options) (Provider, error) {
					return nil, nil
				})
			},
		},
		{
			name: "nil constructor",
			call: func() {
				RegisterProvider(t.Name()+"/nil", nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("RegisterProvider did not panic")
				}
			}()
			tt.call()
		})
	}
}
