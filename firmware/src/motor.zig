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

    var core1_stack: [1024]u32 = undefined;
    multicore.launch_core1_with_stack(recv, &core1_stack);

    inline for (&.{ uart_tx_pin, uart_rx_pin }) |pin| {
        pin.set_function(.uart);
    }

    uart.apply(.{
        .baud_rate = baud_rate,
        .clock_config = rp2xxx.clock_config,
    });

    var buf: [13]u8 = .{0} ** 13;
    while (true) {
        uart.read_blocking(&buf, null) catch {
            // You need to clear UART errors before making a new transaction
            uart.clear_errors();
            blink(10);
            continue;
        };

        x[0] = std.mem.bytesToValue(job, buf[0..13]);
        multicore.fifo.write_blocking(0);
        buf = .{0} ** 13;
    }
}

fn recv() void {
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

    var stepper = A4988.init(.{
        .dir_pin = dp.digital_io(),
        .step_pin = sp.digital_io(),
        .ms1_pin = ms1.digital_io(),
        .ms2_pin = ms2.digital_io(),
        .ms3_pin = ms3.digital_io(),
        .clock_device = cd.clock_device(),
    });

    stepper.begin(300, 16) catch {
        blink(100);
        return;
    };

    const constant_profile = stepper_driver.Speed_Profile.constant_speed;
    //const linear_profile = stepper_driver.Speed_Profile{ .linear_speed = .{ .accel = 8000, .decel = 8000 } };
    stepper.set_speed_profile(constant_profile);

    blink(50);

    while (true) {
        const i = multicore.fifo.read_blocking();
        led.toggle();
        //stepper.set_rpm(x[i].rpm);
        //_ = try stepper.set_microstep(x[i].microstep);
        stepper.move(x[i].steps) catch {
            blink(20);
        };
    }
}

fn blink(n: usize) void {
    for (0..n) |_| {
        led.toggle();
        time.sleep_ms(50);
    }
}
