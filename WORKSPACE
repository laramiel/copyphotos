
http_archive(
    name = "io_bazel_rules_go",
    url = "https://github.com/bazelbuild/rules_go/releases/download/0.8.0/rules_go-0.8.0.tar.gz",
    sha256 = "8eaf2e62811169d9cf511209153effcb132826cea708b2f75d4dd5f9e57ea2aa",
)

load("@io_bazel_rules_go//go:def.bzl", "go_rules_dependencies", "go_register_toolchains", "go_repository")

go_rules_dependencies()
go_register_toolchains()

new_git_repository(
  name = "goexif",
  remote = "https://github.com/rwcarlsen/goexif.git",
  commit = "709fab3",
  build_file = "BUILD.goexif",
)

#go_repository(
#  name = "com_github_rwcarlsen_goexif",
#  importpath = "github.com/rwcarlsen/goexif",
#  commit = "709fab3",
#)
