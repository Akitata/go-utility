package logid

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// Pre-computed lookup tables for performance optimization
var (
	// Lookup table for two-digit numbers (00-99)
	twoDigitLookup [100][2]byte
	
	// Lookup table for three-digit numbers (000-999)  
	threeDigitLookup [1000][3]byte
	
	// Buffer pool for reusing byte slices
	bufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 55) // Full logid length
		},
	}
)

// Initialize lookup tables at package load time
func init() {
	// Initialize two-digit lookup table
	for i := 0; i < 100; i++ {
		twoDigitLookup[i][0] = byte('0' + i/10)
		twoDigitLookup[i][1] = byte('0' + i%10)
	}
	
	// Initialize three-digit lookup table
	for i := 0; i < 1000; i++ {
		threeDigitLookup[i][0] = byte('0' + i/100)
		threeDigitLookup[i][1] = byte('0' + (i/10)%10)
		threeDigitLookup[i][2] = byte('0' + i%10)
	}
}

// XorShift64 provides a fast pseudo-random number generator
type XorShift64 struct {
	state uint64
}

// NewXorShift64 creates a new XorShift64 generator with time-based seed
func NewXorShift64() *XorShift64 {
	return &XorShift64{
		state: uint64(time.Now().UnixNano()),
	}
}

// Next generates the next random number
func (x *XorShift64) Next() uint64 {
	x.state ^= x.state << 13
	x.state ^= x.state >> 7
	x.state ^= x.state << 17
	return x.state
}

// CachedTimestamp holds cached timestamp components
type CachedTimestamp struct {
	lastSecond   int64
	yearMonth    [6]byte  // YYYYMM
	dayHour      [4]byte  // DDHH
	minuteSecond [4]byte  // MMSS
}

// Generator represents a logid generator instance with optimizations
type Generator struct {
	// IP address components (atomic for lock-free access)
	ipHex       atomic.Value // string
	lastIPCheck int64        // atomic timestamp of last IP check
	
	// Timestamp caching
	timestampCache atomic.Value // *CachedTimestamp
	
	// Per-goroutine random generators (lock-free)
	rngPool sync.Pool
	
	// Counter for same-millisecond uniqueness
	lastMillisecond int64  // atomic
	counter         uint32 // atomic
}

var (
	defaultGenerator *Generator
	initOnce         sync.Once
)

// init automatically initializes the default generator
func init() {
	GetGenerator()
}

// GetGenerator returns the singleton generator instance
func GetGenerator() *Generator {
	initOnce.Do(func() {
		defaultGenerator = &Generator{
			rngPool: sync.Pool{
				New: func() interface{} {
					return NewXorShift64()
				},
			},
		}
		defaultGenerator.initialize()
	})
	return defaultGenerator
}

// Generate creates a new logid with the format: timestamp + ip_hex + random_hex
func Generate() string {
	return GetGenerator().Generate()
}

// Generate creates a new logid using this generator instance (optimized)
func (g *Generator) Generate() string {
	// Get buffer from pool
	buf := bufferPool.Get().([]byte)
	defer bufferPool.Put(buf)
	
	// Generate timestamp (17 bytes)
	g.getTimestampOptimized(buf[:17])
	
	// Get IP hex (32 bytes)
	ipHex := g.getIPHexOptimized()
	copy(buf[17:49], ipHex)
	
	// Generate random hex (6 bytes)
	randomHex := g.getRandomHexOptimized()
	copy(buf[49:55], randomHex)
	
	// Convert to string using unsafe for zero-copy
	return *(*string)(unsafe.Pointer(&buf))
}

// initialize sets up the generator
func (g *Generator) initialize() {
	g.refreshIPAddress()
	
	// Initialize timestamp cache
	now := time.Now()
	cache := &CachedTimestamp{
		lastSecond: now.Unix(),
	}
	g.updateTimestampCache(cache, now)
	g.timestampCache.Store(cache)
}

// getTimestampOptimized returns current time in YYYYMMDDHHMMSSMMM format using caching and lookup tables
func (g *Generator) getTimestampOptimized(buf []byte) int {
	now := time.Now()
	currentSecond := now.Unix()
	
	// Try to use cached timestamp components
	if cached := g.timestampCache.Load(); cached != nil {
		cache := cached.(*CachedTimestamp)
		if cache.lastSecond == currentSecond {
			// Use cached components, only update milliseconds
			copy(buf[0:6], cache.yearMonth[:])
			copy(buf[6:10], cache.dayHour[:])
			copy(buf[10:14], cache.minuteSecond[:])
			
			// Only calculate milliseconds
			millisecond := now.Nanosecond() / 1000000
			ms := threeDigitLookup[millisecond]
			copy(buf[14:17], ms[:])
			return 17
		}
	}
	
	// Cache miss or expired, recalculate and update cache
	cache := &CachedTimestamp{
		lastSecond: currentSecond,
	}
	g.updateTimestampCache(cache, now)
	g.timestampCache.Store(cache)
	
	// Copy to buffer
	copy(buf[0:6], cache.yearMonth[:])
	copy(buf[6:10], cache.dayHour[:])
	copy(buf[10:14], cache.minuteSecond[:])
	
	// Add milliseconds
	millisecond := now.Nanosecond() / 1000000
	ms := threeDigitLookup[millisecond]
	copy(buf[14:17], ms[:])
	
	return 17
}

// updateTimestampCache updates the cached timestamp components
func (g *Generator) updateTimestampCache(cache *CachedTimestamp, now time.Time) {
	year := now.Year()
	month := int(now.Month())
	day := now.Day()
	hour := now.Hour()
	minute := now.Minute()
	second := now.Second()
	
	// Year (4 digits) + Month (2 digits)
	cache.yearMonth[0] = byte('0' + year/1000)
	cache.yearMonth[1] = byte('0' + (year/100)%10)
	cache.yearMonth[2] = byte('0' + (year/10)%10)
	cache.yearMonth[3] = byte('0' + year%10)
	monthBytes := twoDigitLookup[month]
	cache.yearMonth[4] = monthBytes[0]
	cache.yearMonth[5] = monthBytes[1]
	
	// Day (2 digits) + Hour (2 digits)
	dayBytes := twoDigitLookup[day]
	cache.dayHour[0] = dayBytes[0]
	cache.dayHour[1] = dayBytes[1]
	hourBytes := twoDigitLookup[hour]
	cache.dayHour[2] = hourBytes[0]
	cache.dayHour[3] = hourBytes[1]
	
	// Minute (2 digits) + Second (2 digits)
	minuteBytes := twoDigitLookup[minute]
	cache.minuteSecond[0] = minuteBytes[0]
	cache.minuteSecond[1] = minuteBytes[1]
	secondBytes := twoDigitLookup[second]
	cache.minuteSecond[2] = secondBytes[0]
	cache.minuteSecond[3] = secondBytes[1]
}

// getIPHexOptimized returns the cached IP address in hex format with atomic access
func (g *Generator) getIPHexOptimized() string {
	// Check if IP needs refresh (every 5 minutes)
	lastCheck := atomic.LoadInt64(&g.lastIPCheck)
	now := time.Now().Unix()
	
	if now-lastCheck > 300 { // 5 minutes
		// Try to update (only one goroutine will succeed)
		if atomic.CompareAndSwapInt64(&g.lastIPCheck, lastCheck, now) {
			go g.refreshIPAddress() // Async refresh
		}
	}
	
	// Return current cached value
	if ipHex := g.ipHex.Load(); ipHex != nil {
		return ipHex.(string)
	}
	
	// Fallback if not initialized
	return "7f000001000000000000000000000000" // localhost fallback
}

// refreshIPAddress updates the cached IP address atomically
func (g *Generator) refreshIPAddress() {
	ip := g.getPreferredIP()
	ipHex := g.ipToHex(ip)
	g.ipHex.Store(ipHex)
}

// getRandomHexOptimized generates a 24-bit random number using XorShift algorithm with proper concurrency handling
func (g *Generator) getRandomHexOptimized() string {
	now := time.Now()
	currentMillisecond := now.UnixMilli()
	
	// Atomic operations for counter management
	for {
		lastMs := atomic.LoadInt64(&g.lastMillisecond)
		
		if currentMillisecond == lastMs {
			// Same millisecond, increment counter atomically
			counter := atomic.AddUint32(&g.counter, 1)
			return fmt.Sprintf("%06x", counter&0xFFFFFF)
		} else {
			// New millisecond, try to update timestamp and reset counter
			if atomic.CompareAndSwapInt64(&g.lastMillisecond, lastMs, currentMillisecond) {
				atomic.StoreUint32(&g.counter, 0)
				// Use XorShift for random generation
				rng := g.rngPool.Get().(*XorShift64)
				randomValue := rng.Next() & 0xFFFFFF // 24 bits
				g.rngPool.Put(rng)
				
				return fmt.Sprintf("%06x", randomValue)
			}
			// If CAS failed, another goroutine updated the timestamp, retry
		}
	}
}

// getPreferredIP returns the preferred IP address (internal network first, then public)
func (g *Generator) getPreferredIP() net.IP {
	interfaces, err := net.Interfaces()
	if err != nil {
		// Fallback to localhost if we can't get interfaces
		return net.ParseIP("127.0.0.1")
	}
	
	var publicIP net.IP
	
	for _, iface := range interfaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			default:
				continue
			}
			
			// Skip loopback addresses
			if ip.IsLoopback() {
				continue
			}
			
			// Prefer IPv4 over IPv6 for consistency
			if ip.To4() != nil {
				// Check if it's a private IP (internal network)
				if g.isPrivateIP(ip) {
					return ip
				}
				// Store public IPv4 as fallback
				if publicIP == nil {
					publicIP = ip
				}
			} else if ip.To16() != nil && publicIP == nil {
				// Store IPv6 as fallback if no IPv4 found
				if g.isPrivateIP(ip) {
					return ip
				}
				if publicIP == nil {
					publicIP = ip
				}
			}
		}
	}
	
	// Return public IP if no private IP found
	if publicIP != nil {
		return publicIP
	}
	
	// Ultimate fallback
	return net.ParseIP("127.0.0.1")
}

// isPrivateIP checks if an IP address is in private network ranges
func (g *Generator) isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	
	// IPv4 private ranges
	if ip.To4() != nil {
		return ip.IsPrivate()
	}
	
	// IPv6 private ranges
	if ip.To16() != nil {
		// Check for IPv6 unique local addresses (fc00::/7)
		return ip.IsPrivate()
	}
	
	return false
}

// ipToHex converts IP address to hex string with consistent length
func (g *Generator) ipToHex(ip net.IP) string {
	if ip.To4() != nil {
		// IPv4: 4 bytes = 8 hex characters, pad to 32 characters for consistency
		ipv4 := ip.To4()
		return fmt.Sprintf("%08x%024x", 
			uint32(ipv4[0])<<24|uint32(ipv4[1])<<16|uint32(ipv4[2])<<8|uint32(ipv4[3]),
			0) // Pad with zeros to match IPv6 length
	} else if ip.To16() != nil {
		// IPv6: 16 bytes = 32 hex characters
		ipv6 := ip.To16()
		return fmt.Sprintf("%032x", ipv6)
	}
	
	// Fallback: return zeros
	return fmt.Sprintf("%032x", 0)
}

// getTimestamp returns current time in YYYYMMDDHHMMSSMMM format (legacy method for compatibility)
func (g *Generator) getTimestamp() string {
	buf := make([]byte, 17)
	g.getTimestampOptimized(buf)
	return string(buf)
}

// getIPHex returns the cached IP address in hex format (legacy method for compatibility)
func (g *Generator) getIPHex() string {
	return g.getIPHexOptimized()
}

// getRandomHex generates a 24-bit random number in hex format with uniqueness guarantee (legacy method for compatibility)
func (g *Generator) getRandomHex() string {
	return g.getRandomHexOptimized()
}

// GetCurrentIP returns the current cached IP address for debugging
func (g *Generator) GetCurrentIP() string {
	if ipHex := g.ipHex.Load(); ipHex != nil {
		return ipHex.(string)
	}
	return "not initialized"
}

// ForceRefreshIP forces a refresh of the IP address cache
func (g *Generator) ForceRefreshIP() {
	g.refreshIPAddress()
}