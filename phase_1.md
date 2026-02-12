Phase 1: The Append-Only Log (Storage Layer)

  This phase is the foundation of your event store. Let me explain each concept in depth.

  ---
  1. Sequential I/O & File Handling

  Why Append-Only?

  Event stores are immutable by design. Once an event is written, it's never modified or deleted. This gives us:

  - Audit trail: Complete history of everything that happened
  - Temporal queries: "What was the state at time X?"
  - Simpler concurrency: No update conflicts, only append conflicts

  Sequential vs Random Writes

  Sequential writes (appending to end of file) are dramatically faster than random writes (writing at arbitrary positions):

  Sequential: [A][B][C][D][E] → append [F] at end
  Random:     [A][B][C][D][E] → insert [X] between B and C

  Why? Disk heads (HDD) or internal mapping (SSD) work best when writing contiguously. The OS can also batch sequential writes more
  efficiently.

  File Descriptors and Key Operations

  In Go, you'll work with os.File. Key operations:
  ┌────────────┬───────────────┬───────────────────────────────────────────────┐
  │ Operation  │  Go Function  │                    Purpose                    │
  ├────────────┼───────────────┼───────────────────────────────────────────────┤
  │ Open       │ os.OpenFile() │ Get a file handle with specific flags         │
  ├────────────┼───────────────┼───────────────────────────────────────────────┤
  │ Seek       │ file.Seek()   │ Move the read/write cursor to a byte position │
  ├────────────┼───────────────┼───────────────────────────────────────────────┤
  │ Write      │ file.Write()  │ Write bytes at current cursor position        │
  ├────────────┼───────────────┼───────────────────────────────────────────────┤
  │ Read       │ file.Read()   │ Read bytes from current cursor position       │
  ├────────────┼───────────────┼───────────────────────────────────────────────┤
  │ Sync/Flush │ file.Sync()   │ Force OS to write buffered data to disk       │
  └────────────┴───────────────┴───────────────────────────────────────────────┘
  File Open Flags

  os.O_CREATE  // Create file if it doesn't exist
  os.O_RDWR    // Open for reading and writing
  os.O_APPEND  // All writes go to end of file (atomic positioning)
  os.O_SYNC    // Synchronous I/O (slow but durable)

  The Durability Problem

  When you call Write(), data goes to the OS buffer, not directly to disk:

  Your Code → OS Buffer (RAM) → Disk
                ↑
           Crash here = data loss

  file.Sync() forces the OS to flush to disk. Trade-off: durability vs performance.

  ---
  2. Record Format: How to Structure Data on Disk

  The Problem with Raw Data

  If you just write raw bytes:
  [event1 bytes][event2 bytes][event3 bytes]

  How do you know where one event ends and another begins?

  Solution 1: Delimiter-Based (Line-Delimited JSON)

  {"type":"OrderCreated","id":1}\n
  {"type":"ItemAdded","id":2}\n

  Pros: Human-readable, easy to debug with cat
  Cons: What if your data contains newlines? Slow to parse.

  Solution 2: Length-Prefixed Binary (Recommended)

  [4 bytes: length][N bytes: data][4 bytes: length][N bytes: data]...

  Example:
  [00 00 00 05][H][e][l][l][o][00 00 00 03][F][o][o]
       ↑           ↑              ↑          ↑
    length=5    "Hello"       length=3     "Foo"

  Pros: Fast parsing, handles any data
  Cons: Not human-readable

  ---
  3. Handling Corruption: Checksums

  The Crash Problem

  What if power fails mid-write?

  Writing: [length=100][data.................
                                ↑
                           CRASH HERE

  On restart, you have a partial record. Reading it will give garbage.

  Solution: CRC32 Checksums

  Wrap each record:

  [4 bytes: length][4 bytes: CRC32 checksum][N bytes: data]

  CRC32 is a fast hash that detects corruption. On read:
  1. Read the length
  2. Read the checksum
  3. Read the data
  4. Compute CRC32 of data
  5. If computed != stored → record is corrupted, discard it

  Go has this built-in: hash/crc32 package.

  ---
  4. Physical Addressing

  Byte Offsets as Pointers

  In databases, a "pointer" to a record is often just its byte position in the file:

  Position 0:    [Record A - 50 bytes]
  Position 50:   [Record B - 30 bytes]
  Position 80:   [Record C - 45 bytes]
  Position 125:  [Record D - ...]

  If you know Record B starts at position 50, you can:
  file.Seek(50, io.SeekStart)  // Jump directly to byte 50
  // Read Record B

  This is O(1) random access - no scanning needed.

  Why Return Positions?

  When you append() a record, return its byte offset:

  func (l *Log) Append(data []byte) (position int64, err error)

  Later, you can build indexes that map StreamID → []positions, enabling fast lookups.

  ---
  5. Complete Record Layout

  Putting it all together, each record on disk looks like:

  ┌─────────────────────────────────────────────────────────┐
  │  Length (4 bytes)  │  CRC32 (4 bytes)  │  Data (N bytes) │
  └─────────────────────────────────────────────────────────┘
          ↑                    ↑                   ↑
     Big-endian          Checksum of          Your event
     uint32              data bytes           payload

  Header size: 8 bytes (length + checksum)
  Total record size: 8 + len(data) bytes

  ---
  6. Key Go Packages You'll Use
  ┌─────────────────┬────────────────────────────────────────────┐
  │     Package     │                  Purpose                   │
  ├─────────────────┼────────────────────────────────────────────┤
  │ os              │ File operations                            │
  ├─────────────────┼────────────────────────────────────────────┤
  │ io              │ SeekStart, SeekEnd constants               │
  ├─────────────────┼────────────────────────────────────────────┤
  │ encoding/binary │ Convert integers to/from bytes (BigEndian) │
  ├─────────────────┼────────────────────────────────────────────┤
  │ hash/crc32      │ Compute checksums                          │
  ├─────────────────┼────────────────────────────────────────────┤
  │ errors          │ Error handling                             │
  └─────────────────┴────────────────────────────────────────────┘
  ---
  Summary: What You'll Build

  Log struct
  ├── file: *os.File           // The append-only log file
  ├── Append(data) → position  // Write record, return byte offset
  ├── ReadAt(position) → data  // Read single record at position
  ├── ReadAll() → []records    // Iterate through all valid records
  └── Close()                  // Clean up