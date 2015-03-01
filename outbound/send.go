package outbound

import (
	"fmt"

	"github.com/miekg/dns"
)

func ResolveDomain(domain string) (string, error) {
	m := dns.Msg{}
	m.SetQuestion(domain+".", dns.TypeMX)
	m.RecursionDesired = true

	r, _, err := dns.Client{}.Exchange(m, "8.8.8.8:53")
	if err != nil {
		return nil, err
	}

	if r.Rcode != dns.RCodeSuccess {
		return fmt.Errorf("DNS query did not succeed, code %d", r.Rcode)
	}

	for _, a := range r.Answer {
		if mx, ok := a.(*dns.MX); ok {

		}
	}
}
