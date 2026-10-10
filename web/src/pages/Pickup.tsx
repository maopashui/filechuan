import { useEffect, useState } from 'react'
import { Alert, Button, Card, Form, Input, Typography, message } from 'antd'
import { Link, useSearchParams } from 'react-router-dom'
import dayjs from 'dayjs'
import { verifyPickup } from '../api/client'
import Logo from '../components/Logo'

function formatSize(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

export default function Pickup() {
  const [params] = useSearchParams()
  const [info, setInfo] = useState<{
    name: string
    size: number
    expires: string
    url: string
  } | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const token = params.get('token')
    if (token) {
      setInfo({
        name: '分享文件',
        size: 0,
        expires: '',
        url: `/api/v1/pickup/${encodeURIComponent(token)}/download`,
      })
    }
  }, [params])

  return (
    <div className="page narrow">
      <Card>
        <div style={{ textAlign: 'center', marginBottom: 16 }}>
          <Logo size={56} />
          <Typography.Title level={3}>取件</Typography.Title>
          <Typography.Paragraph type="secondary">输入取件码下载文件</Typography.Paragraph>
        </div>

        <Form
          layout="vertical"
          onFinish={async (v) => {
            setError(null)
            setInfo(null)
            try {
              const r = await verifyPickup(v.code)
              setInfo({
                name: r.original_name,
                size: r.size,
                expires: r.expires_at,
                url: r.download_url,
              })
            } catch {
              setError('取件码无效、已过期或已撤销')
              message.error('取件失败')
            }
          }}
        >
          <Form.Item name="code" label="取件码" rules={[{ required: true }]}>
            <Input placeholder="例如 ABCD2345" autoCapitalize="characters" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block>
            验证取件码
          </Button>
        </Form>

        {error && <Alert style={{ marginTop: 16 }} type="error" showIcon message={error} />}

        {info && (
          <div style={{ marginTop: 20 }}>
            <Typography.Title level={5}>{info.name}</Typography.Title>
            {info.size > 0 && <Typography.Text type="secondary">大小 {formatSize(info.size)}</Typography.Text>}
            {info.expires && (
              <div>
                <Typography.Text type="secondary">
                  到期 {dayjs(info.expires).format('YYYY-MM-DD HH:mm')}
                </Typography.Text>
              </div>
            )}
            <Button type="primary" href={info.url} style={{ marginTop: 12 }} block>
              下载文件
            </Button>
          </div>
        )}

        <div style={{ marginTop: 16, textAlign: 'center' }}>
          <Link to="/">返回首页</Link>
        </div>
      </Card>
    </div>
  )
}
