const std = @import("std");
const microzig = @import("microzig");
const ptime = rp2xxx.time;
const time = microzig.drivers.time;

const rp2xxx = microzig.hal;
const gpio = rp2xxx.gpio;

const led = gpio.num(25);
const uart = rp2xxx.uart.instance.num(0);
const baud_rate = 115200;
const uart_tx_pin = gpio.num(0);
const uart_rx_pin = gpio.num(1);

pub const job = packed struct {
    rpm: f64,
    steps: i32,
    microstep: u8 = 16,
};

pub fn main() !void {
    led.set_function(.sio);
    led.set_direction(.out);
    led.put(1);

    inline for (&.{ uart_tx_pin, uart_rx_pin }) |pin| {
        pin.set_function(.uart);
    }

    uart.apply(.{
        .baud_rate = baud_rate,
        .clock_config = rp2xxx.clock_config,
    });

    while (true) {
        var data: []u8 = undefined;
        uart.read_blocking(data, null) catch {
            uart.clear_errors();
            continue;
        };

        const j = std.mem.bytesToValue(job, data[0..13]);
        led.put(@intCast(@mod(j.steps, 2)));
    }
}
