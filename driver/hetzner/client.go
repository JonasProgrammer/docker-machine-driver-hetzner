package hetzner

import (
	"encoding/json"
	"fmt"

	"gopkg.in/resty.v1"
)

type Client struct {
	Endpoint string
	Version  string
	client   *resty.Client
}

const (
	hetznerAPIEndpoint = "https://api.hetzner.cloud"
	hetznerAPIVersion  = "v1"
)

func NewClient(token string) *Client {
	client := resty.New()
	client.SetHostURL(hetznerAPIEndpoint + "/" + hetznerAPIVersion)
	client.SetAuthToken(token)
	client.SetHeader("Accept", "application/json")
	client.SetHeader("Content-Type", "application/json")

	return &Client{
		Endpoint: hetznerAPIEndpoint,
		Version:  hetznerAPIVersion,
		client:   client,
	}
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type mappedError struct {
	Error *Error `json:"error"`
}

func extractPrettyError(content []byte, baseError error) error {
	var key mappedError
	err := json.Unmarshal(content, &key)

	if err != nil {
		return baseError
	} else if key.Error != nil {
		return fmt.Errorf("%s: %s", key.Error.Code, key.Error.Message)
	} else {
		return baseError
	}
}

type SSHKey struct {
	Id          int    `json:"id,omitempty"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint,omitempty"`
	PublicKey   string `json:"public_key"`
}

type mappedSSHKey struct {
	Content *SSHKey `json:"ssh_key"`
	Error   *Error  `json:"error"`
}

func (c *Client) CreateSSHKey(name, publicKey string) (*SSHKey, error) {
	resp, err := c.client.R().SetBody(SSHKey{
		Name:      name,
		PublicKey: publicKey,
	}).Post(fmt.Sprintf("ssh_keys"))

	if err != nil {
		return nil, extractPrettyError(resp.Body(), err)
	}

	return extractSSHKey(resp.Body())
}

func (c *Client) GetSSHKey(id int) (*SSHKey, error) {
	resp, err := c.client.R().Get(fmt.Sprintf("ssh_keys/%d", id))

	if err != nil {
		return nil, extractPrettyError(resp.Body(), err)
	}

	return extractSSHKey(resp.Body())
}

func extractSSHKey(content []byte) (*SSHKey, error) {
	var key mappedSSHKey
	err := json.Unmarshal(content, &key)

	if err != nil {
		return nil, err
	} else if key.Error != nil {
		return nil, fmt.Errorf("%s: %s", key.Error.Code, key.Error.Message)
	} else if key.Content == nil {
		return nil, fmt.Errorf("couldn't extract SSH key from malformed response")
	} else {
		return key.Content, nil
	}
}

func (c *Client) DeleteSSHKey(id int) error {
	resp, err := c.client.R().Delete(fmt.Sprintf("ssh_keys/%d", id))

	return extractPrettyError(resp.Body(), err)
}

type Action struct {
	Id       int    `json:"id"`
	Command  string `json:"command"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	Error    *Error `json:"error"`
}

type mappedAction struct {
	Content *Action `json:"action"`
	Error   *Error  `json:"error"`
}

func (c *Client) GetAction(id int) (*Action, error) {
	resp, err := c.client.R().Get(fmt.Sprintf("actions/%d", id))

	if err != nil {
		return nil, extractPrettyError(resp.Body(), err)
	}

	return extractAction(resp.Body())
}

func extractAction(content []byte) (*Action, error) {
	var action mappedAction
	err := json.Unmarshal(content, &action)

	if err != nil {
		return nil, err
	} else if action.Error != nil {
		return nil, fmt.Errorf("%s: %s", action.Error.Code, action.Error.Message)
	} else if action.Content == nil {
		return nil, fmt.Errorf("couldn't extract action from malformed response")
	} else {
		return action.Content, nil
	}
}

type Net struct {
	IP string `json:"ip"`
}

type publicNet struct {
	IPv4 Net `json:"ipv4"`
	IPv6 Net `json:"ipv6"`
}

type Server struct {
	Id        int       `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	PublicNet publicNet `json:"public_net"`
}

type mappedServer struct {
	Content *Server `json:"server"`
	Action  *Action `json:"action"`
	Error   *Error  `json:"error"`
}

type createServerRequest struct {
	Name      string `json:"name"`
	Type      string `json:"server_type"`
	Image     string `json:"image"`
	Location  string `json:"location,omitempty"`
	SSHKeyIDs []int  `json:"ssh_keys,omitempty"`
}

func (c *Client) CreateServer(name, srvtype, image, location string, sshKeyID int) (*Server, *Action, error) {
	resp, err := c.client.R().SetBody(createServerRequest{
		Name:      name,
		Type:      srvtype,
		Image:     image,
		Location:  location,
		SSHKeyIDs: []int{sshKeyID},
	}).Post(fmt.Sprintf("servers"))

	if err != nil {
		return nil, nil, extractPrettyError(resp.Body(), err)
	}

	return extractServerWithAction(resp.Body())
}

func (c *Client) GetServer(id int) (*Server, error) {
	resp, err := c.client.R().Get(fmt.Sprintf("servers/%d", id))

	if err != nil {
		return nil, extractPrettyError(resp.Body(), err)
	}

	return extractServerWithoutAction(resp.Body())
}

func extractServer(content []byte) (*Server, *Action, error) {
	var srv mappedServer
	err := json.Unmarshal(content, &srv)

	if err != nil {
		return nil, nil, err
	} else if srv.Error != nil {
		return nil, nil, fmt.Errorf("%s: %s", srv.Error.Code, srv.Error.Message)
	} else if srv.Content == nil {
		return nil, nil, fmt.Errorf("couldn't extract server from malformed response")
	} else {
		return srv.Content, srv.Action, nil
	}
}

func extractServerWithoutAction(content []byte) (*Server, error) {
	srv, _, err := extractServer(content)
	return srv, err
}

func extractServerWithAction(content []byte) (*Server, *Action, error) {
	srv, act, err := extractServer(content)
	if err == nil && act == nil {
		return nil, nil, fmt.Errorf("couldn't extract action from server response when it was required")
	}
	return srv, act, err
}

func (c *Client) DeleteServer(id int) (*Action, error) {
	resp, err := c.client.R().Delete(fmt.Sprintf("servers/%d", id))

	if err != nil {
		return nil, extractPrettyError(resp.Body(), err)
	}

	return extractAction(resp.Body())
}

func (c *Client) serverAction(id int, action string) (*Action, error) {
	resp, err := c.client.R().Post(fmt.Sprintf("servers/%d/actions/%s", id, action))

	if err != nil {
		return nil, extractPrettyError(resp.Body(), err)
	}

	return extractAction(resp.Body())
}

func (c *Client) PowerOnServer(id int) (*Action, error) {
	return c.serverAction(id, "poweron")
}

func (c *Client) RebootServer(id int) (*Action, error) {
	return c.serverAction(id, "reboot")
}

func (c *Client) ShutdownServer(id int) (*Action, error) {
	return c.serverAction(id, "shutdown")
}

func (c *Client) PowerOffServer(id int) (*Action, error) {
	return c.serverAction(id, "poweroff")
}
