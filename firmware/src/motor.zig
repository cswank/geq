const std = @import("std");
const microzig = @import("microzig");

const rp2040 = microzig.hal;
const time = rp2040.time;
const gpio = rp2040.gpio;
const led = gpio.num(25);

const GPIO_Device = rp2040.drivers.GPIO_Device;
const ClockDevice = rp2040.drivers.ClockDevice;
const sd = microzig.drivers.stepper;
const A4988 = sd.A4988;
const mc = rp2040.multicore;

const uart = rp2040.uart.instance.num(0);
const baud_rate = 115200;
const uart_tx_pin = gpio.num(0);
const uart_rx_pin = gpio.num(1);

const m1_pins = stepper_pins{ .dir = 27, .step = 26, .ms1 = 10, .ms2 = 11, .ms3 = 12 };
const m2_pins = stepper_pins{ .dir = 14, .step = 15, .ms1 = 2, .ms2 = 3, .ms3 = 4 };

var core1_stack: [1024]u32 = undefined;

var stop: bool = false;

pub const motor = packed struct {
    rpm: f64,
    steps: i32,
};

pub const command = packed struct {
    m1: motor,
    m2: motor,
    stop: u8,
};

pub const microzig_options = microzig.Options{
    .log_level = .debug,
    .logFn = rp2040.uart.logFn,
};

var commands: [4]motor = undefined;

pub fn main() !void {
    init();

    var stepper = Stepper.init(m1_pins);
    try stepper.start();

    blink(10);
    led.put(1);

    var buf: [25]u8 = .{0} ** 25;
    var i: u2 = 0;
    while (true) {
        const cmd = recv(i, &buf) catch {
            continue;
        };

        stepper.set_rpm(cmd.m1.rpm);
        stepper.move(cmd.m1.steps) catch {
            sos();
        };

        i +%= 1;
    }
}

fn recv(i: u2, buf: []u8) !command {
    uart.read_blocking(buf, null) catch |err| {
        uart.clear_errors();
        sos();
        return err;
    };

    led.toggle();

    std.log.debug("{X}", .{buf});

    const cmd = std.mem.bytesToValue(command, buf[0..]);
    if (cmd.stop != 0) {
        stop = true;
    } else {
        stop = false;
        commands[i] = cmd.m2;
        mc.fifo.write_blocking(i);
    }

    return cmd;
}

fn motor_2() void {
    var stepper = Stepper.init(m2_pins);
    stepper.start() catch {
        sos();
        return;
    };

    while (true) {
        const i = mc.fifo.read_blocking();
        const m2 = commands[i];
        stepper.set_rpm(m2.rpm);
        stepper.move(m2.steps) catch {
            sos();
        };
    }
}

fn init() void {
    led.set_function(.sio);
    led.set_direction(.out);
    led.put(1);

    uart_tx_pin.set_function(.uart);
    uart_rx_pin.set_function(.uart);
    uart.apply(.{ .baud_rate = baud_rate, .clock_config = rp2040.clock_config });

    rp2040.uart.init_logger(uart);

    mc.launch_core1_with_stack(motor_2, &core1_stack);
}

fn sos() void {
    for (0..4) |_| {
        _blink(3, 100);
        _blink(3, 200);
        _blink(3, 100);
        time.sleep_ms(200);
    }
}

fn blink(n: usize) void {
    _blink(n, 50);
}

fn _blink(n: usize, d: u32) void {
    for (0..n + 1) |_| {
        led.toggle();
        time.sleep_ms(d);
    }
}

const stepper_pins = struct { dir: u6, step: u6, ms1: u6, ms2: u6, ms3: u6 };

const Stepper = struct {
    pins: struct {
        dir: gpio.Pin,
        step: gpio.Pin,
        ms1: gpio.Pin,
        ms2: gpio.Pin,
        ms3: gpio.Pin,
    },
    cd: ClockDevice,
    dir: GPIO_Device = undefined,
    step: GPIO_Device = undefined,
    ms1: GPIO_Device = undefined,
    ms2: GPIO_Device = undefined,
    ms3: GPIO_Device = undefined,
    stepper: sd.A4988 = undefined,

    pub fn init(p: stepper_pins) Stepper {
        var self = Stepper{
            .pins = .{
                .dir = gpio.num(p.dir),
                .step = gpio.num(p.step),
                .ms1 = gpio.num(p.ms1),
                .ms2 = gpio.num(p.ms2),
                .ms3 = gpio.num(p.ms3),
            },
            .cd = ClockDevice{},
        };

        self.pins.dir.set_function(.sio);
        self.pins.step.set_function(.sio);
        self.pins.ms1.set_function(.sio);
        self.pins.ms2.set_function(.sio);
        self.pins.ms3.set_function(.sio);

        self.dir = GPIO_Device.init(self.pins.dir);
        self.step = GPIO_Device.init(self.pins.step);
        self.ms1 = GPIO_Device.init(self.pins.ms1);
        self.ms2 = GPIO_Device.init(self.pins.ms2);
        self.ms3 = GPIO_Device.init(self.pins.ms3);

        return self;
    }

    pub fn start(self: *Stepper) !void {
        self.stepper = A4988.init(.{
            .dir_pin = self.dir.digital_io(),
            .step_pin = self.step.digital_io(),
            .ms1_pin = self.ms1.digital_io(),
            .ms2_pin = self.ms2.digital_io(),
            .ms3_pin = self.ms3.digital_io(),
            .clock_device = self.cd.clock_device(),
        });

        self.stepper.set_speed_profile(A4988.Speed_Profile.constant_speed);
        try self.stepper.begin(300, 16);
    }

    pub fn set_rpm(self: *Stepper, rpm: f64) void {
        self.stepper.set_rpm(rpm);
    }

    pub fn move(self: *Stepper, steps: i32) !void {
        var i: i32 = 0;
        self.stepper.start_move(steps);
        var more: bool = true;
        while (more and !stop) {
            more = try self.stepper.next_action();
            i +%= 1;
        }
    }
};
