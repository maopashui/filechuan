import { Alert, Button, Card, Form, Input, Typography, message } from 'antd'
import { useNavigate } from 'react-router-dom'
import Logo from '../components/Logo'
import { useAuth } from '../stores/auth'

export default function Setup() {
  const register = useAuth((s) => s.register)
  const setupNeeded = useAuth((s) => s.setupNeeded)
  const navigate = useNavigate()

  if (setupNeeded !== true) {
    return (
      <div className="page narrow">
        <Alert type="info" showIcon message="管理员已初始化，注册入口已关闭。" />
        <Button style={{ marginTop: 16 }} onClick={() => navigate('/login')}>
          去登录
        </Button>
      </div>
    )
  }

  return (
    <div className="page narrow">
      <Card>
        <div style={{ textAlign: 'center', marginBottom: 16 }}>
          <Logo size={64} />
          <Typography.Title level={3}>创建管理员</Typography.Title>
          <Typography.Paragraph type="secondary">仅可注册一次，请妥善保管密码。</Typography.Paragraph>
        </div>
        <Form
          layout="vertical"
          onFinish={async (v) => {
            try {
              await register(v.username, v.password)
              message.success('创建成功')
              navigate('/admin')
            } catch (e: unknown) {
              message.error('创建失败，可能已初始化')
            }
          }}
        >
          <Form.Item name="username" label="用户名" rules={[{ required: true, min: 3 }]}>
            <Input autoComplete="username" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, min: 8 }]}>
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block>
            创建并登录
          </Button>
        </Form>
      </Card>
    </div>
  )
}
