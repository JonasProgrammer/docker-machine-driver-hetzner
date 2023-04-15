package driver

const labelNamespace = "docker-machine"

func (d *Driver) labelName(name string) string {
	return labelNamespace + "/" + name
}
