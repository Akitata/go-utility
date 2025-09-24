# LogID Generator

A high-performance, thread-safe log ID generator for Go applications that creates unique identifiers with embedded timestamp, server identification, and randomness.

## Overview

The LogID generator creates 55-character unique identifiers with the following format:
```
YYYYMMDDHHMMSSMMM + IP_HEX(32) + RANDOM_HEX(6)
```

Example: `202509250056335490a0a0a0200000000000000000000000000397909`

## Features

- **Guaranteed Uniqueness**: Combines timestamp precision, server IP, and randomness
- **High Performance**: ~382ns per generation, enhanced for concurrent usage
- **Thread-Safe**: Full concurrent safety with lock-free atomic operations
- **Auto-Initialization**: No manual setup required, works out of the box
- **Network Adaptive**: Automatically detects and caches IP addresses
- **Cross-Platform**: Supports both IPv4 and IPv6 networks
- **Memory Efficient**: Buffer pooling and zero-copy string conversion
- **Fast Random Generation**: XorShift algorithm for high-speed randomness

## Installation

```bash
go get github.com/akitata/utility/logid
```

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/akitata/utility/logid"
)

func main() {
    // Generate a unique log ID
    id := logid.Generate()
    fmt.Println("Generated ID:", id)
    
    // Use in logging context
    fmt.Printf("[%s] Processing request\n", id)
}
```

## Performance Characteristics

### Benchmarks
```
BenchmarkGenerate-4                3,285,620    382.5 ns/op    39 B/op    2 allocs/op
BenchmarkGenerateParallel-4        6,421,148    162.2 ns/op    39 B/op    2 allocs/op
BenchmarkTimestampGeneration-4    20,393,742     57.6 ns/op     0 B/op    0 allocs/op
BenchmarkRandomGeneration-4        7,321,384    181.6 ns/op    15 B/op    1 allocs/op
BenchmarkXorShift-4              315,798,640      3.8 ns/op     0 B/op    0 allocs/op
```

## Uniqueness Guarantees

### Temporal Uniqueness
- Millisecond-precision timestamps ensure time-based uniqueness
- Atomic counter mechanism handles multiple generations within same millisecond
- Lock-free implementation for high-concurrency scenarios

### Spatial Uniqueness
- IP address component differentiates between servers/containers
- Supports both IPv4 and IPv6 deployments
- Consistent 32-character hex format regardless of IP version
- Automatic network interface detection with fallback mechanisms

### Randomness
- 24-bit random space provides 16M+ possibilities per millisecond per server
- XorShift64 pseudo-random number generation for speed
- Deterministic counter fallback for high-frequency generation
- Per-goroutine random generator pooling for concurrent safety

## Thread Safety

The generator is fully thread-safe with enhanced concurrent performance:

- **Atomic Operations**: Lock-free IP cache and counter management
- **Buffer Pooling**: Thread-safe buffer reuse with sync.Pool
- **Per-Goroutine RNG**: Isolated random generators for each goroutine
- **Zero Contention**: Most operations avoid locking entirely

## Error Handling

The generator includes comprehensive error handling:

- **Network Failures**: Falls back to localhost IP
- **Random Generation**: Falls back to time-based pseudo-random
- **Interface Errors**: Graceful degradation to default values
