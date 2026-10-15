go_env := "GOCACHE=" + justfile_directory() + "/build/.gocache"

build_linux_x64:
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 {{ go_env }} go build -v -o build/linux/x64/ossync ./ossync

build_windows_x64:
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 {{ go_env }} go build -v -o build/windows/x64/ossync.exe ./ossync

build_macos_x64:
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 {{ go_env }} go build -v -o build/macos/x64/ossync ./ossync

build_macos_arm64:
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 {{ go_env }} go build -v -o build/macos/arm64/ossync ./ossync

build: build_linux_x64 build_windows_x64 build_macos_x64 build_macos_arm64

run *args:
    CGO_ENABLED=0 {{ go_env }} go run ./ossync {{ args }}

clean:
    rm -rf build
