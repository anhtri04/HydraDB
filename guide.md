
This is a roadmap designed to take you from "writing to a file" to a "production-grade Event Store."

We will break this into 6 Phases. In each phase, you will learn a specific concept (Theory) and then immediately implement it (Practice). Do not move to the next phase until the current one works.

Phase 1: The Append-Only Log (Storage Layer)
Goal: Build a system that can persist data to disk and read it back, ensuring data survives a restart.

1. Sequential I/O & File Handling

Theory: Learn about File Descriptors, seek(), flush(), and why sequential writes are faster than random writes. Study "Line Delimited JSON" vs "Binary Formats" (Protobuf/BSON).

Build: Create a Log class.

Method: append(data): Opens the file, jumps to the end, writes the bytes, adds a delimiter (or length prefix).

Method: read_all(): Opens the file, reads from start to finish, and returns a list of records.

Challenge: What happens if your program crashes halfway through writing a record?

Fix: Implement Checksums (CRC32). Wrap every record in a header: [Length][Checksum][Data]. When reading, if the checksum fails, discard the corrupted tail.

2. Physical Addressing

Theory: Understand "Byte Offsets." In a database, a "pointer" is often just an integer representing a byte position in a file.

Build: Modify append to return the position (integer) where the event was written.

Method: read_at(position): Seek directly to that byte and read one record.

Phase 2: The Stream Index (Query Layer)
Goal: Read events for a specific "User" or "Order" without scanning the entire file.

3. In-Memory Hash Maps

Theory: Hash Maps. We need to map a StreamID (string) to a list of Positions (integers).

Build: Create an Index class.

On startup, read the entire log file. Populate a Map<String, List<Long>> in RAM.

Key: StreamID (e.g., "order-123")

Value: [0, 150, 400] (Byte offsets where this stream's events live).

Optimization: This Startup time will eventually become too slow.

Fix: Build a Sparse Index or keep the index in a separate file that you update as you write.

4. Global vs. Stream Ordering

Theory: The difference between "Global Position" (Order of arrival) and "Stream Version" (Order within the entity).

Build: Update your storage struct. Every event now needs:

GlobalPosition (1, 2, 3, 4...) -> The physical order in the file.

StreamVersion (0, 1, 2...) -> The logical order for that specific ID.

Phase 3: The Guardian (Consistency Layer)
Goal: Prevent two users from changing the same entity at the same time.

5. Optimistic Concurrency Control (OCC)

Theory: Race conditions. What happens if two requests try to append version 5 to stream User-A at the same time?

Build: Implement the AppendToStream logic:

Accept parameter: ExpectedVersion.

Check: if (CurrentVersion != ExpectedVersion) throw WrongExpectedVersionException.

Crucial: This check and the subsequent write must be Atomic. Use a mutex/lock around the write operation for now.

6. Idempotency

Theory: Network retries. If a client sends a request, times out, and sends it again, you shouldn't write the event twice.

Build: Add a check. If the incoming event ID already exists in this stream, return Success (or AlreadyExists) but do not write it again.

Phase 4: The Interface (Networking Layer)
Goal: Move from a library code to a standalone server application.

7. Protocol Design

Theory: TCP vs HTTP/gRPC. For an Event Store, gRPC or raw TCP is often preferred for performance, but HTTP is easier to debug.

Build: Build a simple server wrapper.

POST /streams/{streamId} with Body { events: [...], expectedVersion: 5 }

GET /streams/{streamId}?from=0

8. Streaming Response

Theory: Chunked Transfer Encoding. You cannot load 1 million events into RAM to send them back.

Build: Implement a generator/iterator that reads one event from disk, writes it to the socket, flushes, and repeats.

Phase 5: The Observer (Subscription Layer)
Goal: The "Killer Feature" of Event Sourcing—pushing changes to clients.

9. The Broadcaster (Volatile Subscriptions)

Theory: Observer Pattern / Pub-Sub.

Build:

Maintain a list of active TCP connections/WebSockets.

When a write is successful (in Phase 3), iterate through the connections and push the new event data to them.

10. Catch-Up Subscriptions

Theory: "Fill the gap." Clients might disconnect and reconnect.

Build:

Client sends: Subscribe(StartFrom: 500).

Server logic:

Read historical events 500 -> CurrentHead. Send them.

Seamlessly switch to the "Broadcaster" (Live) mode for any new incoming events.

Phase 6: Production Hardening (DevOps Layer)
Goal: Ensure the system doesn't crash or fill up the disk.

11. Compaction & Scavenging

Theory: Log-Structured Merge Trees (LSM) or Garbage Collection.

Build:

Implement "Soft Deletes" (Write a StreamDeleted event).

Create a background thread that copies active events to a new file and deletes the old large file (scavenging).

12. Snapshots

Theory: Read Performance. Replaying 1 million events to build a User object is slow.

Build: Create a separate "Snapshot Store" (just a Key-Value store).

Every 100 events, calculate the state and save it here.

On read: Load Snapshot + Replay only the subsequent events.

Recommended Learning Path for Compatibility
Start with simple files. Don't use a database engine (like SQLite) to store events; use raw files. It forces you to learn the mechanics of append.

Use a binary format early. While JSON is readable, parsing it is slow. Try using a simple binary format (e.g., [4 bytes length][bytes payload]) early in Phase 1.

Single Threaded Writer. To keep Phase 3 (Concurrency) simple, start by having only one thread allowed to write to the file. Use a queue to feed this thread. This solves 90% of your race conditions without complex locking.