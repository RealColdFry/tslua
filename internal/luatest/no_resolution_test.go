package luatest

import (
	"strings"
	"testing"
)

func TestNoResolutionAnnotation_PreservesRequirePath(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		import { value } from "@NoResolution:json";
		export const result = value;
	`, Opts{
		ExtraFiles: map[string]string{
			"node_modules/noresolution/index.d.ts": `
				/** @noResolution */
				declare module "@NoResolution:json" {
					export const value: string;
				}
			`,
		},
	})

	lua := mainLua(t, results)
	if !strings.Contains(lua, `require("@NoResolution:json")`) {
		t.Fatalf("expected require path to be preserved, got:\n%s", lua)
	}

	deps := results[0].Dependencies
	for _, dep := range deps {
		if dep.RequirePath == "@NoResolution:json" {
			if dep.ResolvedPath != "" {
				t.Fatalf("expected @noResolution dependency to have empty ResolvedPath, got %q", dep.ResolvedPath)
			}
			return
		}
	}
	t.Fatalf("did not find dependency for @NoResolution:json, got %#v", deps)
}
