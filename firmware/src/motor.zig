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

pub const motor = packed struct {
    rpm: f64,
    steps: i32,
};

pub const job = packed struct {
    m1: motor,
    m2: motor,
};

pub const microzig_options = microzig.Options{
    .log_level = .debug,
    .logFn = rp2xxx.uart.logFn,
};

var x: [3]motor = undefined;

pub fn main() !void {
    init();

    var core1_stack: [900]u32 = undefined;
    multicore.launch_core1_with_stack(motor_2, &core1_stack);
    multicore.fifo.drain();

    var stepper = Stepper.init(.{ .dir = 17, .step = 16, .ms1 = 21, .ms2 = 20, .ms3 = 19 });
    stepper.start();

    blink(20);

    var buf: [24]u8 = .{0} ** 24;
    var i: u8 = 0;

    while (true) {
        uart.read_blocking(&buf, null) catch {
            uart.clear_errors();
            blink(10);
            continue;
        };

        std.log.debug("{x}", .{buf});

        const jb = std.mem.bytesToValue(job, buf[0..24]);
        x[i] = jb.m2;
        multicore.fifo.write_blocking(i);
        stepper.set_rpm(jb.m1.rpm);
        stepper.move(jb.m1.steps) catch {};

        i += 1;
        if (i == 3) {
            i = 0;
        }
    }
}

fn motor_2() void {
    var stepper = Stepper.init(.{ .dir = 14, .step = 15, .ms1 = 2, .ms2 = 3, .ms3 = 4 });
    stepper.start();

    while (true) {
        const i = multicore.fifo.read_blocking();
        const m2 = x[i];
        led.toggle();
        stepper.set_rpm(m2.rpm);
        stepper.move(m2.steps) catch {
            blink(100);
        };
    }
}

fn init() void {
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

    rp2xxx.uart.init_logger(uart);
}

fn blink(n: usize) void {
    for (0..n) |_| {
        led.toggle();
        time.sleep_ms(50);
    }
}

const Pins = struct {
    dir: gpio.Pin,
    step: gpio.Pin,
    ms1: gpio.Pin,
    ms2: gpio.Pin,
    ms3: gpio.Pin,
};

const Stepper = struct {
    pins: Pins,
    cd: ClockDevice,
    dir: GPIO_Device = undefined,
    step: GPIO_Device = undefined,
    ms1: GPIO_Device = undefined,
    ms2: GPIO_Device = undefined,
    ms3: GPIO_Device = undefined,
    stepper: stepper_driver.Stepper(stepper_driver.A4988) = undefined,

    pub fn init(p: struct { dir: u6, step: u6, ms1: u6, ms2: u6, ms3: u6 }) Stepper {
        var pins = Pins{
            .dir = gpio.num(p.dir),
            .step = gpio.num(p.step),
            .ms1 = gpio.num(p.ms1),
            .ms2 = gpio.num(p.ms2),
            .ms3 = gpio.num(p.ms3),
        };

        pins.dir.set_function(.sio);
        pins.step.set_function(.sio);
        pins.ms1.set_function(.sio);
        pins.ms2.set_function(.sio);
        pins.ms3.set_function(.sio);

        var self = Stepper{
            .pins = pins,
            .cd = ClockDevice{},
        };

        self.dir = GPIO_Device.init(self.pins.dir);
        self.step = GPIO_Device.init(self.pins.step);
        self.ms1 = GPIO_Device.init(self.pins.ms1);
        self.ms2 = GPIO_Device.init(self.pins.ms2);
        self.ms3 = GPIO_Device.init(self.pins.ms3);

        return self;
    }

    pub fn start(self: *Stepper) void {
        self.stepper = A4988.init(.{
            .dir_pin = self.dir.digital_io(),
            .step_pin = self.step.digital_io(),
            .ms1_pin = self.ms1.digital_io(),
            .ms2_pin = self.ms2.digital_io(),
            .ms3_pin = self.ms3.digital_io(),
            .clock_device = self.cd.clock_device(),
        });

        self.stepper.set_speed_profile(stepper_driver.Speed_Profile.constant_speed);
        self.stepper.begin(300, 16) catch {
            blink(100);
        };
    }

    pub fn set_rpm(self: *Stepper, rpm: f64) void {
        self.stepper.set_rpm(rpm);
    }

    pub fn move(self: *Stepper, steps: i32) !void {
        try self.stepper.move(steps);
    }
};
