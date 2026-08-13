package httpdownload

import (
	"fmt"
	"net/http"
)

func respError(resp *http.Response) error {
	return fmt.Errorf("server returned %s", resp.Status)
}
