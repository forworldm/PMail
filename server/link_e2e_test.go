package main

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Jinnrry/pmail/dto/response"
	"github.com/spf13/cast"
)

// 以下用例覆盖"主账户 / 关联账户"功能：
// 管理员建立授权关系后，主账户可以切换到关联账户身份读写邮件，
// 未授权的切换必须被拒绝。

const (
	linkPrimaryAccount = "user1"
	linkLinkedAccount  = "user2"
)

// postWithHeader 带自定义请求头发起 POST，复用同一个 httpClient 以保留登录会话
func postWithHeader(t *testing.T, url, body string, header map[string]string) *response.Response {
	t.Helper()
	req, err := http.NewRequest("POST", url, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	ret, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer ret.Body.Close()
	data, err := readResponse(ret.Body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// getWithHeader 带自定义请求头发起 GET
func getWithHeader(t *testing.T, url string, header map[string]string) *response.Response {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	ret, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer ret.Body.Close()
	data, err := readResponse(ret.Body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// actingHeader 身份切换请求头，值固定为账号名（不做任何类型猜测）
func actingHeader(account string) map[string]string {
	return map[string]string{"X-Pmail-Act-As": account}
}

// testLinkAddByNonAdmin 普通用户不能建立授权关系
func testLinkAddByNonAdmin(t *testing.T) {
	// 当前会话是 user1（由调用方保证已登录）
	data := postWithHeader(t, TestHost+"/api/link/add",
		`{"primary":"user1","linked":"user2"}`, nil)
	if data.ErrorNo != 405 {
		t.Errorf("non-admin should not create link, got errorNo=%d", data.ErrorNo)
	}
	t.Logf("testLinkAddByNonAdmin Success! errorNo=%d", data.ErrorNo)
}

// testLinkAddByAdmin 管理员建立 user1 -> user2 的授权关系
func testLinkAddByAdmin(t *testing.T) {
	// 先切回管理员会话
	loginAs(t, "testCase", "testCase")

	data := postWithHeader(t, TestHost+"/api/link/add",
		fmt.Sprintf(`{"primary":%q,"linked":%q}`, linkPrimaryAccount, linkLinkedAccount), nil)
	if data.ErrorNo != 0 {
		t.Errorf("admin create link failed: %+v", data)
	}
	t.Logf("testLinkAddByAdmin Success! %+v", data)

	// 重复建立必须被拒绝
	dup := postWithHeader(t, TestHost+"/api/link/add",
		fmt.Sprintf(`{"primary":%q,"linked":%q}`, linkPrimaryAccount, linkLinkedAccount), nil)
	if dup.ErrorNo == 0 {
		t.Error("duplicated link should be rejected")
	}

	// 自关联必须被拒绝
	self := postWithHeader(t, TestHost+"/api/link/add",
		fmt.Sprintf(`{"primary":%q,"linked":%q}`, linkPrimaryAccount, linkPrimaryAccount), nil)
	if self.ErrorNo == 0 {
		t.Error("self link should be rejected")
	}

	// 成环必须被拒绝
	cycle := postWithHeader(t, TestHost+"/api/link/add",
		fmt.Sprintf(`{"primary":%q,"linked":%q}`, linkLinkedAccount, linkPrimaryAccount), nil)
	if cycle.ErrorNo == 0 {
		t.Error("reverse link should be rejected")
	}
}

// testLinkListAsPrimary 主账户能看到自己的关联账户
func testLinkListAsPrimary(t *testing.T) {
	loginAs(t, linkPrimaryAccount, "user1")

	data := postWithHeader(t, TestHost+"/api/link/list", `{}`, nil)
	if data.ErrorNo != 0 {
		t.Errorf("link list failed: %+v", data)
	}
	dt := data.Data.(map[string]interface{})
	items := dt["linked_account"].([]interface{})
	found := false
	for _, item := range items {
		m := item.(map[string]interface{})
		if cast.ToString(m["account"]) == linkLinkedAccount {
			found = true
		}
	}
	if !found {
		t.Errorf("linked account %q not found in %+v", linkLinkedAccount, items)
	}
	t.Logf("testLinkListAsPrimary Success! %+v", items)
}

// testUserInfoAsLinked 切换到关联账户身份后，用户信息应反映新身份并保留真实登录者
func testUserInfoAsLinked(t *testing.T) {
	data := getWithHeader(t, TestHost+"/api/user/info", actingHeader(linkLinkedAccount))
	if data.ErrorNo != 0 {
		t.Errorf("user info failed: %+v", data)
	}
	dt := data.Data.(map[string]interface{})
	if cast.ToString(dt["account"]) != linkLinkedAccount {
		t.Errorf("account = %v, want %s", dt["account"], linkLinkedAccount)
	}
	if cast.ToString(dt["real_account"]) != linkPrimaryAccount {
		t.Errorf("real_account = %v, want %s", dt["real_account"], linkPrimaryAccount)
	}
	if cast.ToBool(dt["is_acting"]) != true {
		t.Errorf("is_acting = %v, want true", dt["is_acting"])
	}
	t.Logf("testUserInfoAsLinked Success! %+v", dt)
}

// listCount 取出邮件列表接口返回的邮件数量
func listCount(t *testing.T, data *response.Response) int {
	t.Helper()
	if data.ErrorNo != 0 {
		t.Fatalf("email list failed: %+v", data)
	}
	dt := data.Data.(map[string]interface{})
	if dt["list"] == nil {
		return 0
	}
	return len(dt["list"].([]interface{}))
}

// testEmailListAsLinked 主账户以关联账户身份只能看到关联账户的邮件。
// 用"发一封信前后计数变化"来做确定性断言，避免依赖前序用例留下的邮箱状态。
func testEmailListAsLinked(t *testing.T) {
	// 注意：此时尚未执行 testModifyPasswordKeepsRealAccount，主账户密码仍是初始值
	loginAs(t, linkPrimaryAccount, "user1")

	primaryBefore := listCount(t, postWithHeader(t, TestHost+"/api/email/list", `{}`, nil))
	linkedBefore := listCount(t, postWithHeader(t, TestHost+"/api/email/list", `{}`, actingHeader(linkLinkedAccount)))

	// 主账户以本人身份给关联账户发一封信
	sendBody := fmt.Sprintf(`
	{
		"from": {"name": "primary", "email": "%s@test.domain"},
		"to": [{"name": "linked", "email": "%s@test.domain"}],
		"cc": [],
		"subject": "隔离性验证邮件",
		"text": "隔离性验证邮件",
		"html": "<div>隔离性验证邮件</div>"
	}`, linkPrimaryAccount, linkLinkedAccount)
	if data := postWithHeader(t, TestHost+"/api/email/send", sendBody, nil); data.ErrorNo != 0 {
		t.Fatalf("send email failed: %+v", data)
	}
	time.Sleep(3 * time.Second)

	linkedAfter := listCount(t, postWithHeader(t, TestHost+"/api/email/list", `{}`, actingHeader(linkLinkedAccount)))
	primaryAfter := listCount(t, postWithHeader(t, TestHost+"/api/email/list", `{}`, nil))

	// 邮件进了关联账户的收件箱
	if linkedAfter != linkedBefore+1 {
		t.Errorf("linked mailbox count = %d, want %d", linkedAfter, linkedBefore+1)
	}
	// 但没有泄漏到主账户的收件箱
	if primaryAfter != primaryBefore {
		t.Errorf("primary mailbox count = %d, want %d (leaked)", primaryAfter, primaryBefore)
	}
	t.Logf("testEmailListAsLinked Success! primary=%d linked=%d->%d",
		primaryAfter, linkedBefore, linkedAfter)
}

// testActAsHeaderAcceptsAccountOnly 身份切换请求头只接受账号名：
// 传完整邮箱地址、传用户ID 都必须被拒绝，绝不猜测数据类型。
func testActAsHeaderAcceptsAccountOnly(t *testing.T) {
	for _, ref := range []string{
		linkLinkedAccount + "@test.domain", // 完整邮箱地址
		"USER2@test.domain",                // 大写邮箱地址
		"999",                              // 用户ID
	} {
		bad := map[string]string{"X-Pmail-Act-As": ref}
		data := getWithHeader(t, TestHost+"/api/user/info", bad)
		if data.ErrorNo != response.NoAccessPrivileges {
			t.Errorf("X-Pmail-Act-As: %q should be rejected, got errorNo=%d", ref, data.ErrorNo)
		}
	}

	// 请求被拒绝后，正常的无头请求必须仍然可用
	ok := getWithHeader(t, TestHost+"/api/user/info", nil)
	if ok.ErrorNo != 0 {
		t.Fatalf("normal request after rejection failed: %+v", ok)
	}
	t.Logf("testActAsHeaderAcceptsAccountOnly Success!")
}

// testActingAsUnrelatedAccountRejected 切换到未授权账户必须被拒绝，且不影响后续请求
func testActingAsUnrelatedAccountRejected(t *testing.T) {
	data := postWithHeader(t, TestHost+"/api/email/list", `{}`, actingHeader("user3"))
	if data.ErrorNo != 405 {
		t.Errorf("acting as unrelated account should be rejected, got errorNo=%d", data.ErrorNo)
	}

	// 被拒绝后，不带请求头的普通请求必须仍然正常
	normal := postWithHeader(t, TestHost+"/api/email/list", `{}`, nil)
	if normal.ErrorNo != 0 {
		t.Errorf("normal request after rejection failed: %+v", normal)
	}
	t.Logf("testActingAsUnrelatedAccountRejected Success!")
}

// testSendAsLinked 以关联账户身份发信，From 必须是该身份
func testSendAsLinked(t *testing.T) {
	body := fmt.Sprintf(`
	{
		"from": {"name": "linked", "email": "%s@test.domain"},
		"to": [{"name": "admin", "email": "admin@test.domain"}],
		"cc": [],
		"subject": "以关联账户身份发送",
		"text": "以关联账户身份发送",
		"html": "<div>以关联账户身份发送</div>"
	}`, linkLinkedAccount)

	data := postWithHeader(t, TestHost+"/api/email/send", body, actingHeader(linkLinkedAccount))
	if data.ErrorNo != 0 {
		t.Errorf("send as linked account failed: %+v", data)
	}
	t.Logf("testSendAsLinked Success!")

	// 以关联账户身份，但 From 使用未授权的地址，必须被拒绝
	badFrom := strings.Replace(body, linkLinkedAccount+"@test.domain", "user3@test.domain", 1)
	bad := postWithHeader(t, TestHost+"/api/email/send", badFrom, actingHeader(linkLinkedAccount))
	if bad.ErrorNo == 0 {
		t.Error("sending with an unauthorized From should be rejected")
	}
}

// testModifyPasswordKeepsRealAccount 以关联账户身份修改密码，只能改自己的。
// 这是关键的回归用例：若错误地使用切换后的身份，改掉的会是关联账户的密码，
// 导致关联账户无法再独立登录。
func testModifyPasswordKeepsRealAccount(t *testing.T) {
	loginAs(t, linkPrimaryAccount, "user1")

	data := postWithHeader(t, TestHost+"/api/settings/modify_password",
		`{"password":"user1New"}`, actingHeader(linkLinkedAccount))
	if data.ErrorNo != 0 {
		t.Errorf("modify password failed: %+v", data)
	}

	// 主账户用新密码能登录
	loginAs(t, linkPrimaryAccount, "user1New")
	// 关联账户用原密码仍然能登录（密码没有被篡改）
	loginAs(t, linkLinkedAccount, "user2New")
	// 切回主账户会话，否则后续以主账户身份发起的请求会变成"以自己身份切换"
	loginAs(t, linkPrimaryAccount, "user1New")

	// 还原主账户密码，避免影响后续用例
	restore := postWithHeader(t, TestHost+"/api/settings/modify_password",
		`{"password":"user1"}`, actingHeader(linkLinkedAccount))
	if restore.ErrorNo != 0 {
		t.Errorf("restore password failed: %+v", restore)
	}
	loginAs(t, linkPrimaryAccount, "user1")

	t.Logf("testModifyPasswordKeepsRealAccount Success!")
}

// testLinkDelByAdmin 管理员解除授权关系后，切换身份必须失效
func testLinkDelByAdmin(t *testing.T) {
	loginAs(t, "testCase", "testCase")

	data := postWithHeader(t, TestHost+"/api/link/del",
		fmt.Sprintf(`{"primary":%q,"linked":%q}`, linkPrimaryAccount, linkLinkedAccount), nil)
	if data.ErrorNo != 0 {
		t.Errorf("admin delete link failed: %+v", data)
	}

	// 切回主账户（密码已被上一个用例还原为初始值），此时切换身份必须被拒绝
	loginAs(t, linkPrimaryAccount, "user1")
	rejected := postWithHeader(t, TestHost+"/api/email/list", `{}`, actingHeader(linkLinkedAccount))
	if rejected.ErrorNo != 405 {
		t.Errorf("acting as linked account after unlink should be rejected, got errorNo=%d", rejected.ErrorNo)
	}

	// 列表接口恢复正常
	normal := postWithHeader(t, TestHost+"/api/email/list", `{}`, nil)
	if normal.ErrorNo != 0 {
		t.Errorf("normal request after unlink failed: %+v", normal)
	}
	t.Logf("testLinkDelByAdmin Success!")
}

// loginAs 切换到指定账户的会话
func loginAs(t *testing.T, account, password string) {
	t.Helper()
	ret, err := httpClient.Post(TestHost+"/api/login", "application/json",
		strings.NewReader(fmt.Sprintf(`{"account":%q,"password":%q}`, account, password)))
	if err != nil {
		t.Fatal(err)
	}
	data, err := readResponse(ret.Body)
	if err != nil {
		t.Fatal(err)
	}
	if data.ErrorNo != 0 {
		t.Fatalf("login as %s failed: %+v", account, data)
	}
}
