import { useState } from 'react';
import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Badge } from '@/components/common/Badge';
import { listAPIKeys, createAPIKey, revokeAPIKey } from '@/api/account';
import { useAuthStore } from '@/store/authStore';
import { useFetch } from '@/hooks/useFetch';
import { formatDate } from '@/utils/format';
import type { APIKey } from '@/types';

export function AccountPage() {
  const user = useAuthStore((s) => s.user);
  const { data: keys, refetch } = useFetch(listAPIKeys, []);
  const [name, setName] = useState('');
  const [newKey, setNewKey] = useState<string | null>(null);

  const create = async () => {
    try {
      const k = await createAPIKey(name || 'Trading API Key');
      setNewKey(k.key || null);
      setName('');
      refetch();
    } catch { /* ignore */ }
  };

  return (
    <Layout>
      <div className="grid h-full grid-cols-1 gap-4 p-4 lg:grid-cols-2">
        <Card title="Profile">
          <div className="space-y-2 p-4">
            <div className="text-sm text-nexa-400">Email</div>
            <div className="text-nexa-100">{user?.email}</div>
            <div className="text-sm text-nexa-400">Role</div>
            <div className="text-nexa-100"><Badge color="accent">{user?.role}</Badge></div>
          </div>
        </Card>
        <Card title="API Keys">
          <div className="space-y-3 p-4">
            <div className="flex gap-2">
              <Input placeholder="Key name" value={name} onChange={(e) => setName(e.target.value)} />
              <Button onClick={create}>Create</Button>
            </div>
            {newKey && (
              <div className="rounded border border-accent/30 bg-accent/10 p-2 text-xs text-accent">
                Save this key now — it will not be shown again.<br />{newKey}
              </div>
            )}
            <table className="w-full text-left text-sm">
              <thead className="text-nexa-400"><tr><th className="py-2">Name</th><th className="py-2">Created</th><th></th></tr></thead>
              <tbody>
                {(keys || []).map((k: APIKey) => (
                  <tr key={k.id} className="border-b border-nexa-700/50">
                    <td className="py-2">{k.name}</td>
                    <td className="py-2 text-nexa-400">{formatDate(k.created_at)}</td>
                    <td className="py-2"><Button size="sm" variant="danger" onClick={() => revokeAPIKey(k.id).then(refetch)}>Revoke</Button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      </div>
    </Layout>
  );
}
