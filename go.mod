module github.com/realcoldfry/tslua

go 1.26

replace (
	github.com/microsoft/typescript-go => ./extern/tsgolint/typescript-go
	github.com/microsoft/typescript-go/shim/ast => ./extern/tsgolint/shim/ast
	github.com/microsoft/typescript-go/shim/bundled => ./extern/tsgolint/shim/bundled
	github.com/microsoft/typescript-go/shim/checker => ./extern/tsgolint/shim/checker
	github.com/microsoft/typescript-go/shim/checkershim => ./shim/checkershim
	github.com/microsoft/typescript-go/shim/compiler => ./shim/compiler
	github.com/microsoft/typescript-go/shim/core => ./extern/tsgolint/shim/core
	github.com/microsoft/typescript-go/shim/diagnosticwriter => ./shim/diagnosticwriter
	github.com/microsoft/typescript-go/shim/incremental => ./shim/incremental
	github.com/microsoft/typescript-go/shim/parser => ./extern/tsgolint/shim/parser
	github.com/microsoft/typescript-go/shim/scanner => ./extern/tsgolint/shim/scanner
	github.com/microsoft/typescript-go/shim/tsoptions => ./extern/tsgolint/shim/tsoptions
	github.com/microsoft/typescript-go/shim/tspath => ./extern/tsgolint/shim/tspath
	github.com/microsoft/typescript-go/shim/vfs => ./extern/tsgolint/shim/vfs
	github.com/microsoft/typescript-go/shim/vfs/cachedvfs => ./extern/tsgolint/shim/vfs/cachedvfs
	github.com/microsoft/typescript-go/shim/vfs/osvfs => ./extern/tsgolint/shim/vfs/osvfs
)

require (
	github.com/fsnotify/fsnotify v1.9.0
	github.com/microsoft/typescript-go/shim/ast v0.0.0-00010101000000-000000000000
	github.com/microsoft/typescript-go/shim/bundled v0.0.0
	github.com/microsoft/typescript-go/shim/checker v0.0.0-00010101000000-000000000000
	github.com/microsoft/typescript-go/shim/checkershim v0.0.0
	github.com/microsoft/typescript-go/shim/compiler v0.0.0
	github.com/microsoft/typescript-go/shim/core v0.0.0
	github.com/microsoft/typescript-go/shim/diagnosticwriter v0.0.0-00010101000000-000000000000
	github.com/microsoft/typescript-go/shim/incremental v0.0.0-00010101000000-000000000000
	github.com/microsoft/typescript-go/shim/scanner v0.0.0-00010101000000-000000000000
	github.com/microsoft/typescript-go/shim/tsoptions v0.0.0
	github.com/microsoft/typescript-go/shim/tspath v0.0.0
	github.com/microsoft/typescript-go/shim/vfs v0.0.0-00010101000000-000000000000
	github.com/microsoft/typescript-go/shim/vfs/cachedvfs v0.0.0
	github.com/microsoft/typescript-go/shim/vfs/osvfs v0.0.0
	github.com/spf13/cobra v1.10.2
)

require (
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/go-json-experiment/json v0.0.0-20260214004413-d219187c3433 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/microsoft/typescript-go v0.0.0-20260309214900-4a59cd78390d // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
)
