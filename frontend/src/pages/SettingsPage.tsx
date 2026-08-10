import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Badge } from '@/components/common/Badge';
import { changePassword } from '@/api/auth';
import { useAuthStore } from '@/store/authStore';
import { toast } from '@/store/toastStore';
import { shortAddr } from '@/utils/format';

export function SettingsPage() {
  const { t } = useTranslation();
  const user = useAuthStore((s) => s.user);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [loading, setLoading] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (loading) return;
    if (next.length < 8) {
      toast.error(t('settings.passwordTooShort'));
      return;
    }
    if (next !== confirm) {
      toast.error(t('settings.passwordMismatch'));
      return;
    }
    setLoading(true);
    try {
      await changePassword({ current_password: current, new_password: next });
      toast.success(t('settings.passwordUpdated'));
      setCurrent('');
      setNext('');
      setConfirm('');
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('settings.updateFailed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Layout>
      <div className="mx-auto grid max-w-5xl grid-cols-1 gap-6 p-4 lg:grid-cols-3">
        <div className="lg:col-span-1">
          <Card title={t('settings.account')}>
            <div className="space-y-4 p-4">
              <div>
                <div className="text-xs uppercase tracking-wider text-nexa-500">{t('settings.email')}</div>
                <div className="mt-1 truncate font-medium text-nexa-100">{user?.email}</div>
              </div>
              <div>
                <div className="text-xs uppercase tracking-wider text-nexa-500">{t('settings.role')}</div>
                <div className="mt-1">
                  <Badge color={user?.role === 'admin' ? 'down' : 'accent'}>{user?.role}</Badge>
                </div>
              </div>
              <div>
                <div className="text-xs uppercase tracking-wider text-nexa-500">{t('settings.userId')}</div>
                <div className="mt-1 break-all font-mono text-xs text-nexa-400">{shortAddr(user?.id, 10, 6)}</div>
              </div>
            </div>
          </Card>
        </div>

        <div className="space-y-6 lg:col-span-2">
          <Card title={t('settings.security')}>
            <form onSubmit={submit} className="space-y-4 p-4">
              <Input
                label={t('settings.currentPassword')}
                type="password"
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
                required
              />
              <Input
                label={t('settings.newPassword')}
                type="password"
                value={next}
                onChange={(e) => setNext(e.target.value)}
                required
                hint="Minimum 8 characters"
              />
              <Input
                label={t('settings.confirmNewPassword')}
                type="password"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                required
              />
              <Button type="submit" isLoading={loading}>
                {t('settings.updatePassword')}
              </Button>
            </form>
          </Card>

          <Card title={t('settings.twoFactor')}>
            <div className="flex items-center justify-between p-4">
              <div>
                <div className="font-medium text-nexa-100">{t('settings.authenticatorApp')}</div>
                <div className="text-sm text-nexa-400">{t('settings.twoFactorDesc')}</div>
              </div>
              <Button variant="secondary" disabled>
                {t('settings.comingSoon')}
              </Button>
            </div>
          </Card>

          <Card title={t('settings.sessions')}>
            <div className="flex items-center justify-between p-4">
              <div>
                <div className="font-medium text-nexa-100">{t('settings.currentSession')}</div>
                <div className="text-sm text-nexa-400">{t('settings.sessionDesc')}</div>
              </div>
              <Badge color="up">
                <span className="mr-1.5 inline-block h-1.5 w-1.5 animate-pulse-soft rounded-full bg-up" />
                {t('settings.active')}
              </Badge>
            </div>
          </Card>
        </div>
      </div>
    </Layout>
  );
}
