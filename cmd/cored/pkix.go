package main

import (
	"chain/core/fileutil"
	"chain/errors"
	"chain/log"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	certsDir            = filepath.Join(fileutil.DefaultDir(), "certs") + string(filepath.Separator)
	certFileExt         = getCertFileExt()
	defaultCertDuration = 10 * 365 * 24 * time.Hour
	defaultCATemplate   = &x509.Certificate{
		BasicConstraintsValid: true,
		IsCA:               true,
		KeyUsage:           x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		SignatureAlgorithm: x509.SHA256WithRSA,
		Subject: pkix.Name{
			CommonName:         "Chain Engineering Mock CA",
			Organization:       []string{"Chain"},
			OrganizationalUnit: []string{"Engineering"},
			Locality:           []string{"San Francisco"},
			Country:            []string{"US"},
		},
		NotBefore:    notBefore(),
		NotAfter:     notBefore().Add(defaultCertDuration).UTC(),
		SerialNumber: generateSerialNumber(),
	}
	defaultCertTemplate = &x509.Certificate{
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", "cored.dev"},
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		SignatureAlgorithm:    x509.SHA256WithRSA,
		Subject: pkix.Name{
			CommonName:         "localhost",
			Organization:       []string{"Chain"},
			OrganizationalUnit: []string{"Engineering"},
			Locality:           []string{"San Francisco"},
			Country:            []string{"US"},
		},
		NotBefore:    notBefore(),
		NotAfter:     notBefore().Add(defaultCertDuration).UTC(),
		SerialNumber: generateSerialNumber(),
	}
)

// generatePKIX checks if a development pkix
// exists on the host and generates one if necessary.
func generatePKIX(ctx context.Context, serverCertPath, serverKeyPath, caPath *string) error {
	*caPath = certsDir + "ca" + certFileExt
	fmt.Println(*caPath)
	exists, err := exist(*caPath)
	if err != nil {
		return err
	}

	*serverCertPath = certsDir + "server" + certFileExt
	*serverKeyPath = certsDir + "server.key"
	*caPath = certsDir + "ca" + certFileExt
	if exists {
		return warn()
	}

	err = os.MkdirAll(certsDir, 0777)
	if err != nil {
		return errors.Wrap(err, "generating development pkix directory")
	}

	ca, key, err := generatePEMKeyPair(certsDir, "ca", defaultCATemplate, nil, 2048, nil)
	if err != nil {
		return errors.Wrap(err, "generating root ca keypair")
	}

	caCert, caKey, err := parsePEMKeypair(ca, key)
	if err != nil {
		return errors.Wrap(err, "parsing root ca keypair")
	}

	_, _, err = generatePEMKeyPair(certsDir, "server", defaultCertTemplate, caCert, 2048, caKey)
	if err != nil {
		return errors.Wrap(err, "generating server keypair")
	}

	_, _, err = generatePEMKeyPair(certsDir, "client", defaultCertTemplate, caCert, 2048, caKey)
	if err != nil {
		return errors.Wrap(err, "generating server keypair")
	}
	return warn()
}

func generatePEMKeyPair(dir, prefix string, req, ca *x509.Certificate, keySize int, priv *rsa.PrivateKey) ([]byte, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		return nil, nil, errors.Wrap(err, "generating private key")
	}

	keyBytes := x509.MarshalPKCS1PrivateKey(key)
	keyBytes = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: keyBytes,
	})

	if ca == nil {
		ca = req
	}
	if priv == nil {
		priv = key
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, req, ca, &key.PublicKey, priv)
	if err != nil {
		return nil, nil, errors.Wrap(err, "generating certificate")
	}
	certBytes = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	err = writeKeyPair(certBytes, keyBytes, dir+prefix+certFileExt, dir+prefix+".key")
	if err != nil {
		return nil, nil, errors.Wrap(err, "writing keypair")
	}
	return certBytes, keyBytes, nil
}

func parsePEMKeypair(c, k []byte) (*x509.Certificate, *rsa.PrivateKey, error) {
	block, _ := pem.Decode(c)
	if block == nil {
		return nil, nil, errors.New("failed to parse certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, errors.New("parsing certificate")
	}

	block, _ = pem.Decode(k)
	if block == nil {
		return nil, nil, errors.New("failed to parse private key")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, errors.New("parsing private key")
	}
	return cert, key, nil
}

func notBefore() time.Time {
	return time.Now().Add(-24 * time.Hour).UTC()
}

func generateSerialNumber() *big.Int {
	// RFC 5280 4.1.2.2:
	// Certificate users MUST be able to handle serialNumber
	// values up to 20 octets.  Conforming CAs MUST NOT use
	// serialNumber values longer than 20 octets.
	serialNumber := make([]byte, 20)
	_, err := io.ReadFull(rand.Reader, serialNumber)
	if err != nil {
		log.Fatalkv(context.Background(), log.KeyError, errors.New(fmt.Sprintf("failed to create certificate serial number: %v", err)))
	}

	// SetBytes interprets buf as the bytes of a big-endian
	// unsigned integer. The leading byte should be masked
	// off to ensure it isn't negative.
	serialNumber[0] &= 0x7F
	return new(big.Int).SetBytes(serialNumber)
}

func writeKeyPair(cBytes, kBytes []byte, cFile, kFile string) error {
	err := ioutil.WriteFile(cFile, cBytes, 0644)
	if err != nil {
		return errors.Wrap(err, "writing "+cFile)
	}

	err = ioutil.WriteFile(kFile, kBytes, 0644)
	if err != nil {
		return errors.Wrap(err, "writing "+kFile)
	}
	return nil
}

func exist(path string) (bool, error) {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		if os.IsPermission(err) {
			return true, nil
		}
		return false, err
	}
	return true, nil
}

func warn() error {
	fmt.Println("\nWARNING: Chain Core requires mutual TLS authentication. Development certs and keys have been generated in", certsDir)
	switch runtime.GOOS {
	case "darwin":
		return warnDarwin()
	case "linux":
		return warnLinux()
	case "windows":
		return warnWindows()
	}
	return nil
}

func warnDarwin() error {
	// retrieve user's login keychain
	cmd := exec.Command("security", "login-keychain")
	out, err := cmd.Output()
	if err != nil {
		return errors.Wrap(err, "finding login keychain")
	}
	loginChain := strings.TrimSpace(string(out))
	loginChain = strings.Replace(loginChain, "\"", "", -1)
	sysChain := "/Library/Keychains/System.keychain"

	installRoot := fmt.Sprintln("sudo", "security", "add-trusted-cert", "-d", "-r", "trustRoot -k", sysChain, certsDir+"ca"+certFileExt)
	installCert := fmt.Sprintln("security", "import", certsDir+"client"+certFileExt, "-k", loginChain)
	installKey := fmt.Sprintln("security", "import", certsDir+"client.key", "-k", loginChain)
	fmt.Println("\nTo install the root CA into the System Keychain run:\n\n\t" + installRoot)
	fmt.Printf("\nTo import the client keypair into your login keychain run the following commands:\n\n\t%s\n\t%s\n\n", installCert, installKey)
	return nil
}

func warnLinux() (err error) {
	cat := exec.Command("/bin/sh", "-c", `cat /etc/*-release`)
	grep := exec.Command("grep", "-e", "^NAME=")
	grep.Stdin, err = cat.StdoutPipe()
	if err != nil {
		return errors.Wrap(err, "creating command pipe")
	}
	err = cat.Start()
	if err != nil {
		return errors.Wrap(err, strings.Join(cat.Args, " "))
	}
	out, err := grep.Output()
	if err != nil {
		return errors.Wrap(err, strings.Join(grep.Args, " "))
	}
	err = cat.Wait()
	if err != nil {
		return errors.Wrap(err, "waiting for command exit")
	}

	os := strings.ToLower(string(out))
	if strings.Contains(os, "alpine") {
		fmt.Println("\nIf running docker, install certs in your host machine's key store.\n")
	}
	return
}

func warnWindows() error {
	// TODO(boymanjor): print instructions to install root ca and client certs
	return nil
}

func getCertFileExt() string {
	if runtime.GOOS == "windows" {
		return ".cer"
	}
	return ".pem"
}
