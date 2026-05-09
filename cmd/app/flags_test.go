package main

import "testing"

func TestRootPersistentFlags(t *testing.T) {
	tests := []struct {
		name      string
		flag      string
		wantShort string
		wantDef   string
	}{
		{name: "config", flag: "config", wantShort: "c", wantDef: "conf/config.yaml"},
		{name: "log-level", flag: "log-level", wantDef: "info"},
		{name: "log-format", flag: "log-format", wantDef: "console"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CONFIG_PATH", "")
			t.Setenv("LOG_LEVEL", "")
			t.Setenv("LOG_FORMAT", "")

			cmd := newRootCmd()
			flag := cmd.PersistentFlags().Lookup(tt.flag)
			if flag == nil {
				t.Fatalf("flag %q not found in PersistentFlags()", tt.flag)
			}
			if flag.Shorthand != tt.wantShort {
				t.Errorf("flag %q shorthand = %q, want %q", tt.flag, flag.Shorthand, tt.wantShort)
			}
			if flag.DefValue != tt.wantDef {
				t.Errorf("flag %q default = %q, want %q", tt.flag, flag.DefValue, tt.wantDef)
			}
		})
	}
}

func TestRootHealthCheckIsLocal(t *testing.T) {
	cmd := newRootCmd()
	if flag := cmd.PersistentFlags().Lookup("health-check"); flag != nil {
		t.Error("--health-check should be a local flag")
	}
	if flag := cmd.LocalFlags().Lookup("health-check"); flag == nil {
		t.Error("--health-check missing from LocalFlags()")
	}
}

func TestRootFlagEnvDefaults(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		flag    string
		wantDef string
	}{
		{name: "CONFIG_PATH feeds config", env: map[string]string{"CONFIG_PATH": "/env/x.yaml"}, flag: "config", wantDef: "/env/x.yaml"},
		{name: "LOG_LEVEL feeds log-level", env: map[string]string{"LOG_LEVEL": "debug"}, flag: "log-level", wantDef: "debug"},
		{name: "LOG_FORMAT feeds log-format", env: map[string]string{"LOG_FORMAT": "json"}, flag: "log-format", wantDef: "json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			cmd := newRootCmd()
			flag := cmd.PersistentFlags().Lookup(tt.flag)
			if flag == nil {
				t.Fatalf("flag %q not found", tt.flag)
			}
			if flag.DefValue != tt.wantDef {
				t.Errorf("flag %q default = %q, want %q", tt.flag, flag.DefValue, tt.wantDef)
			}
		})
	}
}
