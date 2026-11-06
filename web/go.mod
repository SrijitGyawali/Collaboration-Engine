// The web app is not Go code. This file exists only to stop the Go tools
// from descending into web/: "./..." does not cross into a nested module, and
// some npm packages in node_modules ship .go files (for example flatted),
// which would otherwise be picked up by go build, go vet, go test and
// golangci-lint.
module github.com/SrijitGyawali/Collaboration-Engine/web

go 1.26
