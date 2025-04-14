const std = @import("std");
const microzig = @import("microzig");

const rp2xxx = microzig.hal;
const gpio = rp2xxx.gpio;
const time = rp2xxx.time;
const GPIO_Device = rp2xxx.drivers.GPIO_Device;
const ClockDevice = rp2xxx.drivers.ClockDevice;
const stepper_driver = microzig.drivers.stepper;
const A4988 = stepper_driver.Stepper(stepper_driver.A4988);
const multicore = rp2xxx.multicore;

const uart = rp2xxx.uart.instance.num(0);
const baud_rate = 115200;
const uart_tx_pin = gpio.num(0);
const uart_rx_pin = gpio.num(1);

pub const job = struct {
    rpm: f64,
    steps: i32,
    microstep: u8 = 16,
};

var x: [3]job = undefined;

pub fn main() !void {
    multicore.launch_core1(recv);

    const opts = stepper_init();
    var stepper = A4988.init(opts);
    try stepper.begin(400, 16);

    const constant_profile = stepper_driver.Speed_Profile.constant_speed;
    //const linear_profile = stepper_driver.Speed_Profile{ .linear_speed = .{ .accel = 8000, .decel = 8000 } };

    stepper.set_speed_profile(
        constant_profile,
    );

    while (true) {
        const i = multicore.fifo.read_blocking();
        stepper.set_rpm(x[i].speed);
        stepper.set_microstep(x[i].microsteps);
        try stepper.move(x[i].steps);
    }
}

pub fn recv() void {
    uart_init();

    x[0] = job{ .speed = 400, .steps = 10000 };
    x[1] = job{ .speed = 400, .steps = -10000 };

    var data: [1]u8 = .{0};
    while (true) {
        uart.read_blocking(&data, null) catch {
            // You need to clear UART errors before making a new transaction
            uart.clear_errors();
            continue;
        };

        multicore.fifo.write(0);
    }
}

pub fn uart_init() void {
    inline for (&.{ uart_tx_pin, uart_rx_pin }) |pin| {
        pin.set_function(.uart);
    }

    uart.apply(.{
        .baud_rate = baud_rate,
        .clock_config = rp2xxx.clock_config,
    });
}

pub fn stepper_init() stepper_driver.Stepper_Options {
    const led = gpio.num(8);
    var cd = ClockDevice{};
    const dir_pin = gpio.num(14);
    var dp = GPIO_Device.init(dir_pin);
    const step_pin = gpio.num(15);
    var sp = GPIO_Device.init(step_pin);
    const ms1_pin = gpio.num(1);
    const ms2_pin = gpio.num(2);
    const ms3_pin = gpio.num(3);
    var ms1 = GPIO_Device.init(ms1_pin);
    var ms2 = GPIO_Device.init(ms2_pin);
    var ms3 = GPIO_Device.init(ms3_pin);

    led.set_function(.sio);
    led.set_direction(.out);
    dir_pin.set_function(.sio);
    step_pin.set_function(.sio);
    ms1_pin.set_function(.sio);
    ms2_pin.set_function(.sio);
    ms3_pin.set_function(.sio);

    return stepper_driver.Stepper_Options{
        .dir_pin = dp.digital_io(),
        .step_pin = sp.digital_io(),
        .ms1_pin = ms1.digital_io(),
        .ms2_pin = ms2.digital_io(),
        .ms3_pin = ms3.digital_io(),
        .clock_device = cd.clock_device(),
    };
}
