package errors

import "testing"

func TestCodeConstantsCoverExpectedGroups(t *testing.T) {
	cases := map[string]Code{
		"provider": CodeProviderAPIKeyMissing,
		"tool":     CodeToolExecutionTimeout,
		"runtime":  CodeRuntimeContextOverflow,
		"config":   CodeConfigInvalid,
		"plugin":   CodePluginLoadFailed,
	}

	for name, code := range cases {
		if code == "" {
			t.Fatalf("%s code is empty", name)
		}
	}
}

func TestDetectionHelpersMatchByCode(t *testing.T) {
	if !IsAPIKeyMissing(&Error{Code: CodeProviderAPIKeyMissing}) {
		t.Fatalf("expected IsAPIKeyMissing to match code")
	}
	if !IsContextOverflow(&Error{Code: CodeRuntimeContextOverflow}) {
		t.Fatalf("expected IsContextOverflow to match code")
	}
	if !IsToolExecutionTimeout(&Error{Code: CodeToolExecutionTimeout}) {
		t.Fatalf("expected IsToolExecutionTimeout to match code")
	}
}
