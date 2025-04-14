const std = @import("std");
const pg = @import("pg");

pub const log = std.log.scoped(.geq);

const Place = struct { lat: f64, long: f64 };
const Person = struct { name: []u8, phone: []u8 };
const Insured = struct { id: []u8, name: []u8 };

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};

    var pool = pg.Pool.init(gpa.allocator(), .{ .size = 5, .connect = .{
        .port = 5432,
        .host = "127.0.0.1",
    }, .auth = .{
        .username = "parsyl_insurance",
        .database = "parsyl_insurance",
        .password = "s3cr3t",
        .timeout = 10_000,
    } }) catch |err| {
        log.err("Failed to connect: {}", .{err});
        std.posix.exit(1);
    };

    defer pool.deinit();

    const address = try std.net.Address.parseIp("127.0.0.1", 8080);
    var net_server = try address.listen(.{ .reuse_address = true });
    defer net_server.deinit();
    std.log.info("listening at http://localhost:{d}/", .{address.getPort()});

    var cont = true;

    while (cont) {
        var connection = try net_server.accept();
        // const thread = try std.Thread.spawn(.{}, handler, .{&connection});
        // thread.detach();
        cont = try handler(&connection, pool);
    }
}

fn handler(connection: *std.net.Server.Connection, pool: *pg.Pool) !bool {
    defer connection.stream.close();
    var buf: [1024]u8 = undefined;
    var server = std.http.Server.init(connection.*, &buf);
    var request = try server.receiveHead();

    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();

    const rd = try request.reader();
    const body = try rd.readAllAlloc(allocator, 8192);
    const parsed = try std.json.parseFromSlice(Person, allocator, body, .{});

    const user = parsed.value;

    std.debug.print("name: {s}, phone: {s}\n", .{ user.name, user.phone });
    std.debug.print("{s}\t{s}\n", .{ @tagName(request.head.method), request.head.target });

    const insureds = try query(pool, allocator);

    var string = std.ArrayList(u8).init(allocator);
    try std.json.stringify(insureds, .{}, string.writer());
    try request.respond(string.items, .{});

    return std.mem.eql(u8, user.name, "jo");
}

fn query(pool: *pg.Pool, allocator: std.mem.Allocator) ![]Insured {
    var insureds = std.ArrayList(Insured).init(allocator);
    var conn = try pool.acquire();
    defer conn.release();

    var result = try conn.query("select id::text, name from insured", .{});
    defer result.deinit();

    while (try result.next()) |row| {
        try insureds.append(Insured{ .id = row.get([]u8, 0), .name = row.get([]u8, 1) });
    }

    return insureds.items;
}
