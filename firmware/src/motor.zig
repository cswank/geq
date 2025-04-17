const std = @import("std");
const microzig = @import("microzig");

const time = rp2xxx.time;
const rp2xxx = microzig.hal;
const gpio = rp2xxx.gpio;
const led = gpio.num(25);

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

    multicore.launch_core1(recv);

    var cd = ClockDevice{};
    const dir_pin = gpio.num(14);
    dir_pin.set_function(.sio);
    var dp = GPIO_Device.init(dir_pin);
    const step_pin = gpio.num(15);
    step_pin.set_function(.sio);
    var sp = GPIO_Device.init(step_pin);
    const ms1_pin = gpio.num(2);
    const ms2_pin = gpio.num(3);
    const ms3_pin = gpio.num(4);
    ms1_pin.set_function(.sio);
    ms2_pin.set_function(.sio);
    ms3_pin.set_function(.sio);
    var ms1 = GPIO_Device.init(ms1_pin);
    var ms2 = GPIO_Device.init(ms2_pin);
    var ms3 = GPIO_Device.init(ms3_pin);

    const opts = stepper_driver.Stepper_Options{
        .dir_pin = dp.digital_io(),
        .step_pin = sp.digital_io(),
        .ms1_pin = ms1.digital_io(),
        .ms2_pin = ms2.digital_io(),
        .ms3_pin = ms3.digital_io(),
        .clock_device = cd.clock_device(),
    };

    var stepper = A4988.init(opts);
    try stepper.begin(300, 16);

    const constant_profile = stepper_driver.Speed_Profile.constant_speed;
    //const linear_profile = stepper_driver.Speed_Profile{ .linear_speed = .{ .accel = 8000, .decel = 8000 } };

    stepper.set_speed_profile(constant_profile);

    while (true) {
        const i = multicore.fifo.read_blocking();
        //stepper.set_rpm(x[i].rpm);
        //_ = try stepper.set_microstep(x[i].microstep);
        try stepper.move(x[i].steps);
    }
}

pub fn recv() void {
    var data: [13]u8 = undefined;
    var i: i32 = 1;
    while (true) {
        uart.read_blocking(&data, null) catch {
            // You need to clear UART errors before making a new transaction
            uart.clear_errors();
            continue;
        };

        x[0] = job{ .steps = 2000 * i, .rpm = 400 };
        multicore.fifo.write(0);
        //time.sleep_ms(5000);
        i *= -1;
        led.toggle();
    }
}

pub fn uart_init() void {}

pub fn stepper_init() stepper_driver.Stepper_Options {
    var cd = ClockDevice{};
    const dir_pin = gpio.num(14);
    dir_pin.set_function(.sio);
    var dp = GPIO_Device.init(dir_pin);

    const step_pin = gpio.num(15);
    step_pin.set_function(.sio);
    var sp = GPIO_Device.init(step_pin);

    const ms1_pin = gpio.num(2);
    const ms2_pin = gpio.num(3);
    const ms3_pin = gpio.num(4);
    ms1_pin.set_function(.sio);
    ms2_pin.set_function(.sio);
    ms3_pin.set_function(.sio);
    var ms1 = GPIO_Device.init(ms1_pin);
    var ms2 = GPIO_Device.init(ms2_pin);
    var ms3 = GPIO_Device.init(ms3_pin);

    return stepper_driver.Stepper_Options{
        .dir_pin = dp.digital_io(),
        .step_pin = sp.digital_io(),
        .ms1_pin = ms1.digital_io(),
        .ms2_pin = ms2.digital_io(),
        .ms3_pin = ms3.digital_io(),
        .clock_device = cd.clock_device(),
    };
}
