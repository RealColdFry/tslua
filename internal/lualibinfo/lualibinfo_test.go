package lualibinfo

import (
	"strings"
	"testing"
)

func TestResolveMinimalBundle_FooterContainsOnlyDirectExports(t *testing.T) {
	d := &FeatureData{
		ModuleInfo: ModuleInfo{
			"ArrayIndexOf": {Exports: []string{"__TS__ArrayIndexOf"}},
			"ArrayConcat":  {Exports: []string{"__TS__ArrayConcat"}},
		},
		FeatureCode: map[string]string{
			"ArrayIndexOf": "local function __TS__ArrayIndexOf() end\n",
			"ArrayConcat":  "local function __TS__ArrayConcat() end\n",
		},
	}

	out := d.ResolveMinimalBundle([]string{"__TS__ArrayIndexOf"})

	if !strings.HasPrefix(out, "local function __TS__ArrayIndexOf") {
		t.Errorf("expected bundle to start with __TS__ArrayIndexOf, got:\n%s", out)
	}
	if strings.Contains(out, "__TS__ArrayConcat") {
		t.Errorf("expected bundle to NOT contain __TS__ArrayConcat, got:\n%s", out)
	}
	if !strings.Contains(out, "return {\n  __TS__ArrayIndexOf = __TS__ArrayIndexOf\n}") {
		t.Errorf("expected single-export return table, got:\n%s", out)
	}
}

func TestResolveMinimalBundle_TransitiveDepsIncludedButNotExported(t *testing.T) {
	d := &FeatureData{
		ModuleInfo: ModuleInfo{
			"ArrayMap":  {Exports: []string{"__TS__ArrayMap"}, Dependencies: []string{"ArrayFrom"}},
			"ArrayFrom": {Exports: []string{"__TS__ArrayFrom"}},
		},
		FeatureCode: map[string]string{
			"ArrayMap":  "local function __TS__ArrayMap() end\n",
			"ArrayFrom": "local function __TS__ArrayFrom() end\n",
		},
	}

	out := d.ResolveMinimalBundle([]string{"__TS__ArrayMap"})

	if !strings.Contains(out, "local function __TS__ArrayFrom") {
		t.Errorf("expected transitive dep ArrayFrom in bundle body, got:\n%s", out)
	}
	if !strings.Contains(out, "local function __TS__ArrayMap") {
		t.Errorf("expected ArrayMap in bundle body, got:\n%s", out)
	}
	// Footer should only export ArrayMap, not the transitive ArrayFrom.
	footer := out[strings.LastIndex(out, "return {"):]
	if strings.Contains(footer, "__TS__ArrayFrom") {
		t.Errorf("expected footer to NOT export transitive dep, got footer:\n%s", footer)
	}
	if !strings.Contains(footer, "__TS__ArrayMap = __TS__ArrayMap") {
		t.Errorf("expected footer to export ArrayMap, got footer:\n%s", footer)
	}
}

func TestResolveMinimalBundle_MultipleExportsSorted(t *testing.T) {
	d := &FeatureData{
		ModuleInfo: ModuleInfo{
			"Foo": {Exports: []string{"__TS__Foo"}},
			"Bar": {Exports: []string{"__TS__Bar"}},
		},
		FeatureCode: map[string]string{
			"Foo": "local function __TS__Foo() end\n",
			"Bar": "local function __TS__Bar() end\n",
		},
	}

	out := d.ResolveMinimalBundle([]string{"__TS__Foo", "__TS__Bar"})

	footer := out[strings.LastIndex(out, "return {"):]
	// Both should appear; order is sorted by feature name (Bar before Foo).
	barIdx := strings.Index(footer, "__TS__Bar")
	fooIdx := strings.Index(footer, "__TS__Foo")
	if barIdx < 0 || fooIdx < 0 {
		t.Fatalf("expected both __TS__Bar and __TS__Foo in footer, got:\n%s", footer)
	}
	if barIdx > fooIdx {
		t.Errorf("expected __TS__Bar before __TS__Foo (sorted), got:\n%s", footer)
	}
}
