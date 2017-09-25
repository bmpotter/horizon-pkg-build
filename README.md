# horizon-pkg-build

## Introduction

Related Projects:

* `rsapss-tool` (http://github.com/open-horizon/rsapss-tool): The RSA PSS CLI tool and library used by this project
* `exchange-api` (http://github.com/open-horizon/exchange-api)
* `anax` (http://github.com/open-horizon/anax)

## Use

### Building and installing

The default `make` target for this project produces the binary `` [TODO].

The `go install` tool can be used to install the binary in `$GOPATH/bin` _**if**_ you have this project directory in your `$GOPATH`.

### CLI Tool

#### Inline help

[TODO]

#### Sample invocations

It's possible to specify command options with envvars, for instance `--debug` can be enabled like this:

    [TODO]

See the tool's help output for the names of envvars that corresond to command options.

#### Program output

Output from the tool to `stdout` is intended for programmatic use — this is useful when authoring scripts. As a consequence, `stderr` is used to report both informational and error messages. Use the familiar Bash output handling mechanisms (`2>`, `1>`) to isolate `stdout` output.

#### Exit status codes

The following error codes are produced by the CLI tool under described conditions:

 * **3**: CLI invocation error or user input error

### Library

See integration tests like [TODO] for example usage.

## Development

### Make information

The `Makefile` in this project fiddles with the `$GOPATH` and fetches dependencies so that `make` targets can be executed outside of the `$GOPATH`. Some of this tomfoolery is hidden in normal build output. To see `make`'s progress, execute `make` with the argument `verbose=y`.

Notable `make` targets include:

 * `all` (default) - Compile source and produce binary
 * `clean` - Clean build artifacts
 * `lint` - Execute Golang code analysis tools to report ill code style
 * `check` - Execute lint toosl, then unit and integration tests
