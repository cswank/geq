const std = @import("std");
const microzig = @import("microzig");
const ptime = rp2xxx.time;
const time = microzig.drivers.time;

const rp2xxx = microzig.hal;
const gpio = rp2xxx.gpio;

const uart1 = rp2xxx.uart.instance.num(0);
const uart2 = rp2xxx.uart.instance.num(1);
const baud_rate = 115200;
const uart1_tx_pin = gpio.num(0);
const uart2_rx_pin = gpio.num(9);
const mc = rp2xxx.multicore;

const decl_output = gpio.num(12);
const decl_index = gpio.num(13);

const ra_output = gpio.num(14);
const ra_index = gpio.num(15);

pub const microzig_options = microzig.Options{
    .log_level = .debug,
    .logFn = rp2xxx.uart.log,
};

var ra_steps: u16 = undefined;

var core1_stack: [1024]u32 = undefined;
var buf: [256]u8 = .{0} ** 256;
const address: u8 = 0x11;

var timeout = time.Duration.from_ms(100);

pub const message = packed struct {
    sync: u8 = 0,
    address: u8 = 0,
    ra_steps: u16 = 0,
    decl_steps: u16 = 0,
    crc: u8 = 0,
};

pub fn main() !void {
    init();

    while (true) {
        const msg = recv() catch |err| {
            std.log.debug("recv error: {}", .{err});
            continue;
        };

        if (msg.address != address) {
            continue;
        }

        std.log.debug("address: {d}, steps: {d}", .{ msg.address, msg.ra_steps });

        ra_steps = msg.ra_steps;

        mc.fifo.write_blocking(1);

        count(msg.decl_steps, decl_output, decl_index);
    }
}

fn recv() !message {
    try read();

    for (0.., buf) |x, element| {
        if (x < 255 and element == 0x5 and buf[x + 1] == address) {
            return std.mem.bytesToValue(message, buf[x .. x + 8]);
        }
    }

    return message{};
}

fn read() !void {
    buf = .{0} ** 256;

    var to: ?time.Duration = null;

    var idx: usize = 0;
    while (idx < 256) {
        _ = uart2.read_blocking(buf[idx .. idx + 8], to) catch |err| {
            uart2.clear_errors();
            if (err != error.Timeout) {
                return err;
            }
            return;
        };
        to = timeout;
        idx += 8;
    }
}

fn counter() void {
    while (true) {
        _ = mc.fifo.read_blocking();
        count(ra_steps, ra_output, ra_index);
    }
}

fn count(target: u16, output: gpio.Pin, index: gpio.Pin) void {
    std.log.debug("count", .{});
    var i: u32 = 0;
    var state: u1 = 0;

    output.toggle(); //tell controller to start motor

    while (i < target) {
        ptime.sleep_us(100);
        if (index.read() != state) {
            state = 1 - state;
            if (state == 1) {
                i += 1;
                if (target - i == 100) {
                    output.toggle(); //tell controller to slow down
                }
            }
        }
    }

    std.log.debug("stop", .{});
    output.toggle(); //tell controller to stop
}

fn init() void {
    decl_index.set_direction(.in);
    decl_index.set_function(.sio);

    decl_output.set_direction(.out);
    decl_output.set_function(.sio);

    ra_index.set_direction(.in);
    ra_index.set_function(.sio);

    ra_output.set_direction(.out);
    ra_output.set_function(.sio);

    uart1_tx_pin.set_function(.uart);
    uart2_rx_pin.set_function(.uart);

    uart1.apply(.{
        .baud_rate = baud_rate,
        .clock_config = rp2xxx.clock_config,
    });

    uart2.apply(.{
        .baud_rate = baud_rate,
        .clock_config = rp2xxx.clock_config,
    });

    rp2xxx.uart.init_logger(uart1);

    mc.launch_core1_with_stack(counter, &core1_stack);
}

pub fn panic(txt: []const u8, _: ?*std.builtin.StackTrace, _: ?usize) noreturn {
    std.log.err("panic: {s}", .{txt});
    @breakpoint();
    while (true) {}
}
