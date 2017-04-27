load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_prefix")

package(
    default_visibility = ["//visibility:public"],
)

go_prefix("github.com/laramiel/copyphotos/")

go_binary(
    name = "copyphotos",
    srcs = ["copyphotos.go"],
    deps = [
        "@goexif//:tiff",
        "@goexif//:exif",
    ]
)
