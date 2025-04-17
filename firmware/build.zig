const std = @import("std");
const microzig = @import("microzig");

const MicroBuild = microzig.MicroBuild(.{
    .rp2xxx = true,
});

pub fn build(b: *std.Build) void {
    const optimize = b.standardOptimizeOption(.{});
    const mz_dep = b.dependency("microzig", .{});
    const mb = MicroBuild.init(b, mz_dep) orelse return;

    const motor = mb.add_firmware(.{
        .name = "motor",
        .target = mb.ports.rp2xxx.boards.raspberrypi.pico,
        //.optimize = .ReleaseSmall,
        .optimize = optimize,
        //.root_source_file = b.path("src/motor.zig"),
        .root_source_file = b.path("src/uartpoc.zig"),
    });

    // We call this twice to demonstrate that the default binary output for
    // RP2040 is UF2, but we can also output other formats easily
    mb.install_firmware(motor, .{});

    const master = mb.add_firmware(.{
        .name = "master",
        .target = mb.ports.rp2xxx.boards.raspberrypi.pico,
        //.optimize = .ReleaseSmall,
        .optimize = optimize,
        .root_source_file = b.path("src/main.zig"),
    });

    // We call this twice to demonstrate that the default binary output for
    // RP2040 is UF2, but we can also output other formats easily
    mb.install_firmware(master, .{});
}
