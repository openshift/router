package util

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"

	e2e "k8s.io/kubernetes/test/e2e/framework"
)

type SshClient struct {
	User       string
	Host       string
	Port       int
	PrivateKey string
}

func (sshClient *SshClient) getConfig() (*ssh.ClientConfig, error) {
	pemBytes, err := ioutil.ReadFile(sshClient.PrivateKey)
	if err != nil {
		e2e.Logf("Pem byte failed:%v", err)
	}
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		e2e.Logf("Parse key failed:%v", err)
	}
	config := &ssh.ClientConfig{
		User: sshClient.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
	}
	return config, err
}

// Run runs cmd on the remote host.
func (sshClient *SshClient) Run(cmd string) error {
	combinedOutput, err := sshClient.RunOutput(cmd)
	if err != nil {
		return err
	}
	e2e.Logf("Successfully executed cmd '%s' with output:\n%s", cmd, combinedOutput)
	return nil
}

// RunOutput runs cmd on the remote host and returns its combined standard output and standard error.
func (sshClient *SshClient) RunOutput(cmd string) (string, error) {
	config, err := sshClient.getConfig()
	if err != nil {
		return "", fmt.Errorf("failed to get SSH config: %v", err)
	}

	connection, err := ssh.Dial("tcp", fmt.Sprintf("%v:%v", sshClient.Host, sshClient.Port), config)
	if err != nil {
		return "", fmt.Errorf("failed to dial %s:%d: %v", sshClient.Host, sshClient.Port, err)
	}
	defer connection.Close()

	session, err := connection.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %v", err)
	}
	defer session.Close()

	combinedOutputBuffer := NewSynchronizedBuffer()
	session.Stdout = combinedOutputBuffer
	session.Stderr = combinedOutputBuffer

	err = session.Run(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to run cmd '%s': %v\n%s", cmd, err, combinedOutputBuffer.String())
	}
	return combinedOutputBuffer.String(), nil
}

func GetPrivateKey() (string, error) {
	// Search order: SSH_CLOUD_PRIV_KEY -> SSH_PRIV_KEY -> fallback file
	privateKeys := []string{
		os.Getenv("SSH_CLOUD_PRIV_KEY"),
		os.Getenv("SSH_PRIV_KEY"),
		filepath.Join("../internal/config/keys", "openshift-qe.pem"),
	}

	for _, privateKey := range privateKeys {
		if privateKey == "" {
			continue
		}
		if _, err := os.Stat(privateKey); err == nil {
			return privateKey, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to stat private key %q: %w", privateKey, err)
		}
	}

	return "", fmt.Errorf("no valid private key found in env or fallback path")
}

func GetPublicKey() (string, error) {
	publicKey := os.Getenv("SSH_CLOUD_PUB_KEY")
	if publicKey == "" {
		publicKey = filepath.Join("../internal/config/keys/", "openshift-qe.pub")
	}
	if _, err := os.Stat(publicKey); os.IsNotExist(err) {
		return "", fmt.Errorf("public key file not found: %s", publicKey)
	}
	return publicKey, nil
}
