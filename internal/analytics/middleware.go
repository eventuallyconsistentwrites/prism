// Intercept traffic, finds the ip, sanitizes data(removes port no etc), pssses it to downstream tracker
package analytics

import (
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/eventuallyconsistentwrites/prism/internal/tracker"
)

func TrackingMiddleware(tr *tracker.Tracker, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//will extract path visited
		path := r.URL.Path
		ip := ExtractIP(r)
		tr.Track(path, ip)
		next.ServeHTTP(w, r)
	})
}

// helper function to extract IPs
func ExtractIP(r *http.Request) string {
	//since this might run on proxies, look for source IP addresses
	//look for 'X-forwarded-for' or 'X-Real-IP'
	//if not found fallback to r.RemoteAddr

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		//This might return a list of ips, as requests passing through every proxy, each proxy adds xff
		ips := strings.Split(xff, ",")
		ip := strings.TrimSpace(ips[0]) //leftmost IP is the client(source)
		if net.ParseIP(ip) != nil {
			return ip
		}
	}

	//check real ip header
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		if net.ParseIP(ip) != nil {
			return ip
		}
	}

	//fallback to remote addr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	// something might be malformed
	slog.Warn("could not parse remote addr", "remote_addr", r.RemoteAddr)
	return r.RemoteAddr
}
