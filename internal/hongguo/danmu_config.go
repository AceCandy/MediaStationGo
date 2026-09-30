package hongguo

import (
	"errors"
	"strconv"
	"strings"
)

// DanmuAppConfig 仅供固定红果弹幕接口使用，所有参数可空；不得直接返回给浏览器。
type DanmuAppConfig struct {
	Cookie    string            `json:"cookie,omitempty"`
	Token     string            `json:"token,omitempty"`
	UserAgent string            `json:"user_agent,omitempty"`
	DeviceID  string            `json:"device_id,omitempty"`
	IID       string            `json:"iid,omitempty"`
	Query     map[string]string `json:"query,omitempty"`
}

func (c DanmuAppConfig) Configured() map[string]bool {
	return map[string]bool{"cookie": c.Cookie != "", "token": c.Token != "", "user_agent": c.UserAgent != "", "device_id": c.DeviceID != "", "iid": c.IID != "", "query": len(c.Query) > 0}
}

// Validate 拒绝任意主机、签名、时间戳和头注入；错误不包含参数值。
func (c DanmuAppConfig) Validate() error {
	for name, value := range map[string]string{"cookie": c.Cookie, "token": c.Token, "user_agent": c.UserAgent, "device_id": c.DeviceID, "iid": c.IID} {
		if len(value) > 16384 || strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("hongguo_app: invalid " + name)
		}
	}
	for _, id := range []string{c.DeviceID, c.IID} {
		if id != "" && !ValidID(id) {
			return errors.New("hongguo_app: invalid device_id or iid")
		}
	}
	const allowed = " aid app_name version_code version_name manifest_version_code update_version_code channel device_platform os ssmix device_type device_brand language os_api os_version resolution dpi ac "
	for name, value := range c.Query {
		if name == "" || strings.ContainsAny(name, " \t\r\n") || !strings.Contains(allowed, " "+name+" ") || len(value) > 256 || strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("hongguo_app: invalid query parameter")
		}
		if name == "aid" {
			n, err := strconv.ParseUint(value, 10, 31)
			if err != nil || n == 0 {
				return errors.New("hongguo_app: invalid aid")
			}
		}
	}
	return nil
}
