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
    blink();

    inline for (&.{ uart_tx_pin, uart_rx_pin }) |pin| {
        pin.set_function(.uart);
    }

    uart.apply(.{
        .baud_rate = baud_rate,
        .clock_config = rp2xxx.clock_config,
    });

    var i: i32 = 1;
    while (true) {
        const j = job{
            .rpm = 300,
            .steps = 10000 * i,
            .microstep = 16,
        };

        i *= -1;

        const data = std.mem.asBytes(&j);

        uart.write_blocking(data[0..13], null) catch {
            uart.clear_errors();
            blink();
            continue;
        };

        led.toggle();
        ptime.sleep_ms(5000);
    }
}

fn blink() void {
    for (0..10) |_| {
        led.toggle();
        ptime.sleep_ms(50);
    }
}
