# horizon-pkg-build

## Introduction

Related Projects:

* `rsapss-tool` (http://github.com/open-horizon/rsapss-tool): The RSA PSS CLI tool and library used by this project
* `horizon-pkg-fetch` (https://github.com/open-horizon/horizon-pkg-fetch): Horizon Pkg fetch library
* `exchange-api` (http://github.com/open-horizon/exchange-api)
* `anax` (http://github.com/open-horizon/anax)

## Use

### Building and installing

The default `make` target for this project produces the binary `horizon-pkg-build`.

The `go install` tool can be used to install the binary in `$GOPATH/bin` _**if**_ you have this project directory in your `$GOPATH`.

### CLI Tool

#### Inline help

The CLI binary includes help:

    horizon-pkg-build --help

    NAME:
       horizon-pkg-build - Create, validate, and upload Horizon Pkg metadata and parts

    USAGE:
       horizon-pkg-build [global options] command [command options] [arguments...]

    VERSION:
       0.1.0

    COMMANDS:
         create, c  Create a new Horizon Pkg from Docker image files
         help, h    Shows a list of commands or help for one command

    GLOBAL OPTIONS:
       --debug         [$HZNPKG_DEBUG]
       --help, -h     show help
       --version, -v  print the version
    [INFO] Exiting.


#### Sample invocations

To create a complete Horizon Pkg bundle with a JSON metadata file and corresponding signature, issue a command like:

    horizon-pkg-build create --dockerimage 'summit.hovitos.engineering/x86/gt-emu:0.1.0' --dockerimage 'summit.hovitos.engineering/x86/gt-cloudpublisher:0.2.0' --privatekey /tmp/private.key --author 'mdye@us.ibm.com' --parturlbase 'https://images.bluehorizon.network/hzn/images'

The command will process Docker images (saved in the Pkg as *parts*) and output tagged informational log messages to `stderr`. If no errors occur during processing, the tool will output a space-separated three-item list of content written in this order: 1) the name of the pkg content directory containing all serialized parts; 2) the name of the Pkg metadata file; and 3) the name of the Pkg metadata file's signature. All output is written to the provided output directory and the path to that directory is omitted from the program's printed output. Example output:

    [INFO] Created temporary directory for packaging: build-hznpkg-5aecb70187cc9d0277baad3cbb0e0d664479b34c-171297214
    ...
    5aecb70187cc9d0277baad3cbb0e0d664479b34c 5aecb70187cc9d0277baad3cbb0e0d664479b34c.json 5aecb70187cc9d0277baad3cbb0e0d664479b34c.json.sig
    [INFO] Exiting.

It's possible to specify command options with envvars.  See the tool's help output for the names of envvars that corresond to command options.

#### Program output

Output from the tool to `stdout` is intended for programmatic use — this is useful when authoring scripts. As a consequence, `stderr` is used to report both informational and error messages. Use the familiar Bash output handling mechanisms (`2>`, `1>`) to isolate `stdout` output.

#### Exit status codes

The following error codes are produced by the CLI tool under described conditions:

 * **2**: User input error
 * **3**: CLI invocation error

## Package Content

The Horizon Pkg output follows the following rules:

 * The Pkg's own ID (something like `5aecb70187cc9d0277baad3cbb0e0d664479b34c`) is a hash of select content and the time the Pkg was created therefore two packages with identical content, but created at different times, will have different package IDs
 * The Parts in a package have IDs (something like `21f9d1dd0fd9964e3c732f83433d7a93997de90c4a2557ac0f8cd4d894897ffb`) that depend only on the content of the part. One part shared by two Pkgs could be deduplicated on disk
 * A *part*'s signatures and hash are calculated **before** compression. A common compression encoding for Docker image files is `gzip`; to verify the signature of the part, you must start the verify operation after decompression. For example:

        pkg=5aecb70187cc9d0277baad3cbb0e0d664479b34c; part=e26e31a03cd9e340e42edf0a83188a0c8bcea2cb1cee9729b7c69695262c8eb8; gunzip -c ./$pkg/$part.tar.gz  | rsapss-tool verify -k /tmp/public.key -x <(cat $pkg.json | jq -r '.parts[] | select(.id=="'$part'") | .signatures[0]')
        [INFO] Using publickey: /tmp/public.key
        SIGOK
        Signature valid
        [INFO] Exiting.

## Development

### Make information

The `Makefile` in this project fiddles with the `$GOPATH` and fetches dependencies so that `make` targets can be executed outside of the `$GOPATH`. Some of this tomfoolery is hidden in normal build output. To see `make`'s progress, execute `make` with the argument `verbose=y`.

Notable `make` targets include:

 * `all` (default) - Compile source and produce binary
 * `clean` - Clean build artifacts
 * `lint` - Execute Golang code analysis tools to report ill code style
 * `check` - Execute lint toosl, then unit and integration tests
