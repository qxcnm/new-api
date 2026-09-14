import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { PasswordInput } from '@/components/password-input'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import { SettingsSection } from '../components/settings-section'

type SMSConfig = {
  enabled: boolean
  configured: boolean
  access_key_id: string
  sign_name: string
  template_code: string
  has_access_key_secret: boolean
}

const emptyConfig: SMSConfig = {
  enabled: false,
  configured: false,
  access_key_id: '',
  sign_name: '',
  template_code: '',
  has_access_key_secret: false,
}

export function SMSSettingsSection() {
  const { t } = useTranslation()
  const [config, setConfig] = useState<SMSConfig>(emptyConfig)
  const [secret, setSecret] = useState('')
  const [testPhone, setTestPhone] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    api
      .get<{ data: SMSConfig }>('/api/option/sms')
      .then((response) => setConfig({ ...emptyConfig, ...response.data.data }))
      .catch((error) =>
        handleServerError(error, t('Failed to load SMS settings'))
      )
  }, [t])

  const save = async () => {
    try {
      setSaving(true)
      const response = await api.put<{
        success?: boolean
        message?: string
        data?: SMSConfig
      }>('/api/option/sms', {
        ...config,
        access_key_secret: secret,
      })
      if (!response.data?.success || !response.data.data) {
        throw createServerError(response.data, t('Failed to update SMS settings'))
      }
      setConfig({ ...emptyConfig, ...response.data.data })
      setSecret('')
      toast.success(t('Settings updated successfully'))
    } catch (error) {
      handleServerError(error, t('Failed to update SMS settings'))
    } finally {
      setSaving(false)
    }
  }

  const test = async () => {
    try {
      const response = await api.post<{
        success?: boolean
        message?: string
      }>('/api/option/sms/test', { phone_number: testPhone })
      if (!response.data?.success) {
        throw createServerError(response.data, t('Failed to send test SMS'))
      }
      toast.success(t('Test SMS submitted'))
    } catch (error) {
      handleServerError(error, t('Failed to send test SMS'))
    }
  }

  return (
    <SettingsSection title={t('SMS Phone Configuration')}>
      <div className='space-y-4'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Configure Aliyun SMS to allow users to receive quota and system notifications by phone.'
          )}
        </p>
        <div className='flex items-center justify-between rounded-lg border p-3'>
          <Label htmlFor='sms-enabled'>{t('Enable SMS notifications')}</Label>
          <Switch
            id='sms-enabled'
            checked={config.enabled}
            onCheckedChange={(enabled) =>
              setConfig((prev) => ({ ...prev, enabled }))
            }
          />
        </div>
        <div className='grid gap-4 md:grid-cols-2'>
          <div className='space-y-1.5'>
            <Label htmlFor='sms-access-key-id'>AccessKey ID</Label>
            <Input
              id='sms-access-key-id'
              value={config.access_key_id}
              onChange={(e) =>
                setConfig((prev) => ({
                  ...prev,
                  access_key_id: e.target.value,
                }))
              }
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='sms-access-key-secret'>AccessKey Secret</Label>
            <PasswordInput
              id='sms-access-key-secret'
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              placeholder={
                config.has_access_key_secret
                  ? t('Leave empty to keep current secret')
                  : undefined
              }
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='sms-sign-name'>{t('SMS Sign Name')}</Label>
            <Input
              id='sms-sign-name'
              value={config.sign_name}
              onChange={(e) =>
                setConfig((prev) => ({ ...prev, sign_name: e.target.value }))
              }
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='sms-template-code'>{t('SMS Template Code')}</Label>
            <Input
              id='sms-template-code'
              value={config.template_code}
              onChange={(e) =>
                setConfig((prev) => ({
                  ...prev,
                  template_code: e.target.value,
                }))
              }
              placeholder='SMS_123456789'
            />
          </div>
        </div>
        <div className='flex flex-wrap gap-2'>
          <Button type='button' onClick={save} disabled={saving}>
            {t('Save SMS settings')}
          </Button>
          <Input
            className='max-w-xs'
            type='tel'
            inputMode='tel'
            value={testPhone}
            onChange={(e) => setTestPhone(e.target.value)}
            placeholder={t('Test phone number')}
          />
          <Button
            type='button'
            variant='outline'
            onClick={test}
            disabled={
              !config.enabled || !config.configured || !testPhone.trim()
            }
          >
            {t('Send test SMS')}
          </Button>
        </div>
      </div>
    </SettingsSection>
  )
}
