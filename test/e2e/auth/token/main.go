// Command token mints HS256 JWTs for the auth E2E. It lives in the main
// module so the test can `go run` it without a separate build step.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	secret := flag.String("secret", "", "HS256 shared secret")
	subject := flag.String("sub", "alice", "subject claim")
	expiresIn := flag.Duration("exp", time.Hour, "expiry offset from now; negative values mint expired tokens")
	flag.Parse()

	if *secret == "" {
		fmt.Fprintln(os.Stderr, "token: -secret is required")
		os.Exit(1)
	}

	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]interface{}{
		"sub": *subject,
		"exp": float64(time.Now().Add(*expiresIn).Unix()),
	})
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)

	mac := hmac.New(sha256.New, []byte(*secret))
	mac.Write([]byte(signingInput))
	fmt.Println(signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
}
