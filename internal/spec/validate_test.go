package spec

import "testing"

func TestEffectivePermission(t *testing.T) {
	tests := []struct {
		name        string
		permissions []Permission
		want        Permission
		wantError   bool
	}{
		{name: "agent maximum", permissions: []Permission{PermissionCollaborate}, want: PermissionCollaborate},
		{name: "route reduces agent", permissions: []Permission{PermissionCollaborate, PermissionReadonly}, want: PermissionReadonly},
		{name: "harness reduces route", permissions: []Permission{PermissionAutonomous, PermissionCollaborate, PermissionReadwrite}, want: PermissionReadwrite},
		{name: "order independent", permissions: []Permission{PermissionReadonly, PermissionAutonomous}, want: PermissionReadonly},
		{name: "no values", wantError: true},
		{name: "invalid", permissions: []Permission{"admin"}, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := EffectivePermission(test.permissions...)
			if test.wantError {
				if err == nil {
					t.Fatalf("EffectivePermission() = %q, nil; want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("EffectivePermission() error = %v", err)
			}
			if got != test.want {
				t.Errorf("EffectivePermission() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		value     string
		want      int64
		wantError bool
	}{
		{value: "1GB", want: 1_000_000_000},
		{value: "500 MB", want: 500_000_000},
		{value: "1024B", want: 1024},
		{value: "0GB", wantError: true},
		{value: "1GiB", wantError: true},
		{value: "unlimited", wantError: true},
		{value: "9999999999999999999GB", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			got, err := ParseByteSize(test.value)
			if test.wantError {
				if err == nil {
					t.Fatalf("ParseByteSize(%q) = %d, nil; want error", test.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseByteSize(%q) error = %v", test.value, err)
			}
			if got != test.want {
				t.Errorf("ParseByteSize(%q) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}
