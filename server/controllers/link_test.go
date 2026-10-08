package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Jinnrry/pmail/dto/response"
	"github.com/Jinnrry/pmail/utils/context"
)

// decodeResponse 解析统一响应体
func decodeResponse(t *testing.T, body []byte) *response.Response {
	t.Helper()
	var ret response.Response
	if err := json.Unmarshal(body, &ret); err != nil {
		t.Fatalf("unmarshal response failed: %v, body=%s", err, string(body))
	}
	return &ret
}

// TestLinkEndpointsRequireAdmin 验证关联管理接口仅管理员可操作。
// 权限校验发生在任何数据库访问之前，因此无需初始化数据库。
func TestLinkEndpointsRequireAdmin(t *testing.T) {
	cases := []struct {
		name    string
		handler func(*context.Context, http.ResponseWriter, *http.Request)
	}{
		{"LinkAdd", LinkAdd},
		{"LinkDel", LinkDel},
		{"LinkAdminList", LinkAdminList},
	}

	for _, c := range cases {
		t.Run(c.name+"_非管理员被拒绝", func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/link/x", nil)
			ctx := &context.Context{UserID: 5, UserAccount: "primary", RealUserID: 5}

			c.handler(ctx, w, req)

			if w.Code != 200 {
				t.Fatalf("expect http 200, got %d", w.Code)
			}
			ret := decodeResponse(t, w.Body.Bytes())
			if ret.ErrorNo != response.NoAccessPrivileges {
				t.Fatalf("expect errorNo %d, got %d (%s)",
					response.NoAccessPrivileges, ret.ErrorNo, ret.ErrorMsg)
			}
		})
	}
}

// TestLinkAddRequiresLinkedParam 验证缺少必填参数时返回参数错误。
func TestLinkAddRequiresLinkedParam(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/link/add", nil)
	ctx := &context.Context{UserID: 1, UserAccount: "admin", IsAdmin: true, RealUserID: 1}

	LinkAdd(ctx, w, req)

	ret := decodeResponse(t, w.Body.Bytes())
	if ret.ErrorNo != response.ParamsError {
		t.Fatalf("expect errorNo %d, got %d (%s)", response.ParamsError, ret.ErrorNo, ret.ErrorMsg)
	}
}

// TestLinkAddRejectsBadJSON 验证请求体不是合法 JSON 时返回参数错误，
// 而不是把零值结构体继续传下去。
func TestLinkAddRejectsBadJSON(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/link/add", strings.NewReader(`{"primary":`))
	ctx := &context.Context{UserID: 1, UserAccount: "admin", IsAdmin: true, RealUserID: 1}

	LinkAdd(ctx, w, req)

	ret := decodeResponse(t, w.Body.Bytes())
	if ret.ErrorNo != response.ParamsError {
		t.Fatalf("expect errorNo %d, got %d (%s)", response.ParamsError, ret.ErrorNo, ret.ErrorMsg)
	}
}

// TestLinkAddRejectsAccountAsID 验证不通过请求体里的 id 形式操作账户，
// 缺少 primary 参数时必须报参数错误（而不是退化成 "当前登录账户"）。
func TestLinkAddRejectsAccountAsID(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/link/add", strings.NewReader(`{"linked":"linked","primary_id":1,"linked_id":2}`))
	ctx := &context.Context{UserID: 1, UserAccount: "admin", IsAdmin: true, RealUserID: 1}

	LinkAdd(ctx, w, req)

	ret := decodeResponse(t, w.Body.Bytes())
	if ret.ErrorNo != response.ParamsError {
		t.Fatalf("expect errorNo %d, got %d (%s)", response.ParamsError, ret.ErrorNo, ret.ErrorMsg)
	}
}

// TestLinkListOthersRequireAdmin 验证普通用户不能查询他人的关联账户。
func TestLinkListOthersRequireAdmin(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/link/list", strings.NewReader(`{"account":"other"}`))
	ctx := &context.Context{UserID: 5, UserAccount: "primary", RealUserID: 5}

	LinkList(ctx, w, req)

	ret := decodeResponse(t, w.Body.Bytes())
	if ret.ErrorNo != response.NoAccessPrivileges {
		t.Fatalf("expect errorNo %d, got %d (%s)",
			response.NoAccessPrivileges, ret.ErrorNo, ret.ErrorMsg)
	}
}
