<template>
  <div class="settings-card">
    <div class="settings-header">
      <h3>{{ lang.account_management }}</h3>
      <p class="settings-desc">{{ lang.account_management_desc }}</p>
    </div>

    <div class="table-container">
      <el-table :data="userList" class="modern-table" style="width: 100%">
        <el-table-column label="ID" prop="ID" width="80"/>
        <el-table-column :label="lang.account" prop="Account" min-width="150" show-overflow-tooltip/>
        <el-table-column :label="lang.user_name" prop="Name" min-width="120" show-overflow-tooltip/>
        <el-table-column :label="lang.disabled" prop="Disabled" width="120">
          <template #default="scope">
            <el-tag :type="scope.row.Disabled === 1 ? 'info' : 'success'" size="small" effect="plain" class="status-tag">
              {{ scope.row.Disabled === 1 ? lang.disabled : lang.enabled }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="lang.linked_account" min-width="110">
          <template #default="scope">
            <el-tag size="small" effect="plain" type="warning" v-if="linkCountMap[scope.row.Account] > 0">
              {{ linkCountMap[scope.row.Account] }}
            </el-tag>
            <span class="no-link" v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column align="right" width="190">
          <template #header>
            <el-button type="primary" size="small" @click="createUser" class="new-btn" plain>
              <el-icon><Plus/></el-icon> New
            </el-button>
          </template>
          <template #default="scope">
            <el-button size="small" type="primary" text bg @click="handleEdit(scope.$index, scope.row)" class="action-btn">
              Edit
            </el-button>
            <el-button size="small" type="primary" text bg @click="handleManageLink(scope.row)" class="action-btn">
              {{ lang.manage_link }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="pagination-wrapper">
      <el-pagination 
        v-model:current-page="currentPage" 
        small 
        background 
        layout="prev, pager, next"
        :page-count="totalPage" 
        @current-change="reflushList"
      />
    </div>

    <el-dialog v-model="userInfoDialog" :title="title" width="450px" class="premium-dialog">
      <div class="dialog-content">
        <el-form label-position="top">
          <el-form-item :label="lang.account">
            <el-input :disabled="editModel === 'edit'" v-model="editUserInfo.account"/>
          </el-form-item>

          <el-form-item :label="lang.user_name">
            <el-input v-model="editUserInfo.name"/>
          </el-form-item>

          <el-form-item :label="lang.password">
            <el-input :placeholder="lang.resetPwd" v-model="editUserInfo.password" type="password" show-password/>
          </el-form-item>

          <el-form-item>
            <div class="status-switch">
              <span class="switch-label">Status</span>
              <el-switch 
                v-model="editUserInfo.disabled" 
                class="ml-2" 
                :active-text="lang.disabled"
                :inactive-text="lang.enabled"
                active-color="#ef4444"
                inactive-color="#10b981"
              />
            </div>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="userInfoDialog = false">Cancel</el-button>
          <el-button type="primary" @click="submit">Confirm</el-button>
        </span>
      </template>
    </el-dialog>

    <!-- 关联账户管理对话框 -->
    <el-dialog v-model="linkDialog" :title="linkDialogTitle" width="520px" class="premium-dialog">
      <div class="dialog-content">
        <p class="link-desc">{{ lang.link_desc }}</p>

        <div class="link-current">
          <div class="link-subtitle">{{ lang.linked_account }}</div>
          <el-table :data="linkList" class="modern-table" style="width: 100%" max-height="240">
            <el-table-column :label="lang.account" prop="account" min-width="120" show-overflow-tooltip/>
            <el-table-column :label="lang.user_name" prop="name" min-width="100" show-overflow-tooltip/>
            <el-table-column :label="lang.disabled" width="90">
              <template #default="scope">
                <el-tag :type="scope.row.disabled === 1 ? 'info' : 'success'" size="small" effect="plain">
                  {{ scope.row.disabled === 1 ? lang.disabled : lang.enabled }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column align="right" width="80">
              <template #default="scope">
                <el-button size="small" type="danger" text bg @click="removeLink(scope.row)">
                  {{ lang.del_btn }}
                </el-button>
              </template>
            </el-table-column>
          </el-table>
          <div class="link-empty" v-if="linkList.length === 0">{{ lang.no_linked_account }}</div>
        </div>

        <div class="link-add">
          <div class="link-subtitle">{{ lang.add_linked_account }}</div>
          <div class="link-add-row">
            <el-select v-model="newLinkAccount" filterable class="link-add-select" :placeholder="lang.select_account">
              <el-option
                  v-for="item in allUsers"
                  :key="item.ID"
                  :label="item.Account + (item.Name ? ' (' + item.Name + ')' : '')"
                  :value="item.Account"
                  :disabled="item.Account === linkTargetAccount || linkedAccountSet.includes(item.Account)"
              />
            </el-select>
            <el-button type="primary" @click="addLink">{{ lang.submit }}</el-button>
          </div>
        </div>
      </div>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="linkDialog = false">{{ lang.close_btn || 'Close' }}</el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import {computed, reactive, ref} from 'vue'
import lang from '../i18n/i18n';
import {http} from "@/utils/axios";
import {ElNotification} from "element-plus";
import {Plus} from "@element-plus/icons-vue";

const userList = reactive([])

// ---- 关联账户管理 ----
const linkDialog = ref(false)
const linkDialogTitle = ref("")
const linkTargetAccount = ref("")
const linkList = reactive([])
const newLinkAccount = ref("")

const linkedAccountSet = computed(() => linkList.map(item => item.account))

// 下拉框需要全量用户（表格是分页的），单独拉取一份
const allUsers = reactive([])
const loadAllUsers = function () {
  http.post('/api/user/list', {"current_page": 1, "page_size": 500}).then(res => {
    allUsers.length = 0
    if (res.data && res.data["list"]) {
      allUsers.push(...res.data["list"])
    }
  })
}
loadAllUsers()

// 查询授权关系统一用账号名，不使用用户ID
const loadLinks = function (account) {
  http.post('/api/link/list', {"account": account}).then(res => {
    linkList.length = 0
    if (res.errorNo === 0 && res.data && res.data.linked_account) {
      linkList.push(...res.data.linked_account)
    }
  })
}

const handleManageLink = function (row) {
  linkTargetAccount.value = row.Account
  linkDialogTitle.value = lang.manage_link + " - " + row.Account
  newLinkAccount.value = ""
  linkDialog.value = true
  loadLinks(row.Account)
}

const addLink = function () {
  if (newLinkAccount.value === "") {
    return
  }
  http.post('/api/link/add', {
    "primary": linkTargetAccount.value,
    "linked": newLinkAccount.value
  }).then(res => {
    ElNotification({
      title: res.errorNo === 0 ? lang.succ : lang.fail,
      message: res.errorNo === 0 ? "" : res.data,
      type: res.errorNo === 0 ? 'success' : 'error',
    })
    if (res.errorNo === 0) {
      newLinkAccount.value = ""
      loadLinks(linkTargetAccount.value)
      reflushLinkCount()
    }
  })
}

const removeLink = function (row) {
  http.post('/api/link/del', {
    "primary": linkTargetAccount.value,
    "linked": row.account
  }).then(res => {
    ElNotification({
      title: res.errorNo === 0 ? lang.succ : lang.fail,
      message: res.errorNo === 0 ? "" : res.data,
      type: res.errorNo === 0 ? 'success' : 'error',
    })
    if (res.errorNo === 0) {
      loadLinks(linkTargetAccount.value)
      reflushLinkCount()
    }
  })
}

// 统计每个主账户名下的关联账户数量，用于表格展示
const linkCountMap = reactive({})
const reflushLinkCount = function () {
  http.post('/api/link/admin_list', {}).then(res => {
    Object.keys(linkCountMap).forEach(k => delete linkCountMap[k])
    if (res.errorNo === 0 && res.data && res.data.list) {
      res.data.list.forEach(item => {
        linkCountMap[item.primary_account] = (linkCountMap[item.primary_account] || 0) + 1
      })
    }
  })
}
reflushLinkCount()
const currentPage = ref(1)
const totalPage = ref(1)
const userInfoDialog = ref(false)
const editModel = ref("edit")
const editUserInfo = reactive({
  "account": "",
  "name": "",
  "password": "",
  "disabled": false
})
const title = ref(lang.editUser)

const reflushList = function () {
  http.post('/api/user/list', {"current_page": currentPage.value, "page_size": 10}).then(res => {
    userList.length = 0
    totalPage.value = res.data.total_page
    if (res.data["list"]) {
      userList.push(...res.data["list"])
    }
  })
}

const handleEdit = function (idx, row) {
  editUserInfo.account = row.Account
  editUserInfo.name = row.Name
  editUserInfo.disabled = row.Disabled === 1
  editUserInfo.password = ""
  editModel.value = "edit"
  title.value = lang.editUser
  userInfoDialog.value = true
}

const createUser = function () {
  editUserInfo.account = ""
  editUserInfo.name = ""
  editUserInfo.disabled = false
  editUserInfo.password = ""
  editModel.value = "create"
  title.value = lang.newUser
  userInfoDialog.value = true
}

const submit = function () {
  if (editModel.value === 'edit') {
    let newData = {
      "account": editUserInfo.account,
      "username": editUserInfo.name,
      "disabled": editUserInfo.disabled ? 1 : 0
    }
    if (editUserInfo.password !== "") {
      newData["password"] = editUserInfo.password
    }

    http.post('/api/user/edit', newData).then(res => {
      ElNotification({
        title: res.errorNo === 0 ? lang.succ : lang.fail,
        message: res.errorNo === 0 ? "" : res.data,
        type: res.errorNo === 0 ? 'success' : 'error',
      })
      if (res.errorNo === 0) {
        reflushList()
        userInfoDialog.value = false
      }
    })
  } else {
    let newData = {
      "account": editUserInfo.account,
      "username": editUserInfo.name,
      "disabled": editUserInfo.disabled ? 1 : 0,
      "password": editUserInfo.password
    }

    http.post('/api/user/create', newData).then(res => {
      ElNotification({
        title: res.errorNo === 0 ? lang.succ : lang.fail,
        message: res.errorNo === 0 ? "" : res.data,
        type: res.errorNo === 0 ? 'success' : 'error',
      })
      if (res.errorNo === 0) {
        reflushList()
        userInfoDialog.value = false
      }
    })
  }
}

reflushList()
</script>

<style scoped>
.settings-card {
  padding: 0;
}

.settings-header {
  margin-bottom: 24px;
}

.settings-header h3 {
  font-size: 18px;
  font-weight: 600;
  color: var(--pm-text-primary);
  margin: 0 0 8px 0;
}

.settings-desc {
  font-size: 14px;
  color: var(--pm-text-secondary);
  margin: 0;
}

.table-container {
  border: 1px solid var(--pm-border-color);
  border-radius: var(--pm-radius-sm);
  overflow: hidden;
  margin-bottom: 24px;
}

.modern-table :deep(th.el-table__cell) {
  background-color: var(--pm-bg-secondary);
  color: var(--pm-text-secondary);
  font-weight: 600;
}

.status-tag {
  border-radius: var(--pm-radius-sm);
}

.new-btn {
  border-radius: var(--pm-radius-sm);
}

.action-btn {
  border-radius: var(--pm-radius-sm);
  padding: 4px 12px;
}

.pagination-wrapper {
  margin-top: 20px;
  display: flex;
  justify-content: center;
}

/* Dialog Styles */
.premium-dialog :deep(.el-dialog__header) {
  border-bottom: 1px solid var(--pm-border-color);
  padding-bottom: 16px;
  margin-bottom: 20px;
}

.no-link {
  color: var(--pm-text-secondary);
}

.link-desc {
  margin: 0 0 16px;
  font-size: 13px;
  color: var(--pm-text-secondary);
  line-height: 1.6;
}

.link-subtitle {
  font-size: 13px;
  font-weight: 600;
  color: var(--pm-text-primary);
  margin-bottom: 10px;
}

.link-current {
  margin-bottom: 22px;
}

.link-empty {
  padding: 14px 0;
  font-size: 12px;
  color: var(--pm-text-secondary);
  text-align: center;
}

.link-add-row {
  display: flex;
  gap: 10px;
}

.link-add-select {
  flex: 1;
}

.dialog-content {
  padding: 0 8px;
}

.status-switch {
  display: flex;
  align-items: center;
  gap: 16px;
  width: 100%;
  padding: 8px 12px;
  background: var(--pm-bg-secondary);
  border-radius: var(--pm-radius-sm);
  border: 1px solid var(--pm-border-color);
}

.switch-label {
  font-size: 14px;
  font-weight: 500;
  color: var(--pm-text-primary);
}
</style>