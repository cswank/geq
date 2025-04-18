const std = @import("std");

pub const job = packed struct {
    rpm: f64,
    steps: i32,
    microstep: u8 = 16,
};

fn main() void {
    const j = job{
        .rpm = 300,
        .steps = 200,
        .microstep = 16,
    };

    const data = std.mem.asBytes(&j);
    std.debug.print("{X}", data);
}
