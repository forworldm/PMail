import {defineStore} from "pinia";
import {http} from "@/utils/axios";

const ACTING_STORAGE_KEY = 'pmail.acting_account';

const useGlobalStatusStore = defineStore('useGlobalStatusStore', {
    state() {
        return {
            userInfos: {},
            mobileDrawerVisible: false,
            settingsDrawerVisible: false,
            // 可以切换到的关联账户列表（主账户视角）
            linkedAccounts: [],
            // 当前正在以哪个关联账户的账号名操作，null 表示本人
            actingAccount: null
        }
    },
    getters: {
        isLogin(state) {
            return Object.keys(state.userInfos).length !== 0
        },
        // 是否正在以关联账户身份操作
        isActing(state) {
            return state.actingAccount !== null && state.actingAccount !== undefined && state.actingAccount !== ''
        },
        // 当前操作身份的完整地址（用于写信页展示）
        currentAddress(state) {
            const account = state.userInfos.account || ''
            const domains = state.userInfos.domains || []
            if (!account || domains.length === 0) {
                return account
            }
            return account + "@" + domains[0]
        }
    },
    actions: {
        init(callback) {
            let that = this
            http.post("/api/user/info", {}).then(res => {
                if (res.errorNo === 0) {
                    Object.assign(that.userInfos, res.data)
                    that.linkedAccounts = (res.data && res.data.linked_account) || []
                    // 校验本地缓存的身份是否仍然有效，无效则回落到本人
                    const cached = that.readCachedActingAccount()
                    if (cached !== null && !that.linkedAccounts.some(item => item.account === cached)) {
                        that.writeCachedActingAccount(null)
                    } else {
                        that.actingAccount = cached
                    }
                    callback()
                }
            })
        },
        // 切换操作身份：account 为 null 表示切回本人
        setActing(account, callback) {
            this.actingAccount = account
            this.writeCachedActingAccount(account)
            // 重新拉取用户信息，让 userInfos 反映新的操作身份
            this.init(() => {
                if (callback) {
                    callback()
                }
            })
        },
        readCachedActingAccount() {
            try {
                const val = window.localStorage.getItem(ACTING_STORAGE_KEY)
                if (val === null || val === '' || val === 'null' || val === 'undefined') {
                    return null
                }
                return val
            } catch (e) {
                return null
            }
        },
        writeCachedActingAccount(account) {
            try {
                if (account === null || account === undefined || account === '') {
                    window.localStorage.removeItem(ACTING_STORAGE_KEY)
                } else {
                    window.localStorage.setItem(ACTING_STORAGE_KEY, String(account))
                }
            } catch (e) {
                // 隐私模式下 localStorage 不可用，忽略
            }
        }
    }
})


export {useGlobalStatusStore};
