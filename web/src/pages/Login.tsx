import { Button, Card, Form, Input, Typography, message } from 'antd'
import { Link, useNavigate } from 'react-router-dom'
import Logo from '../components/Logo'
import { useAuth } from '../stores/auth'

export default function Login() {
  const login = useAuth((s) => s.login)
  const navigate = useNavigate()

  return (
    <div className="page narrow">
      <Card>
        <div style={{ textAlign: 'center', marginBottom: 16 }}>
          <Logo size={56} />
          <Typography.Title level={3}>管理员登录</Typography.Title>
        </div>
        <Form
          layout="vertical"
          onFinish={async (v) => {
            try {
              await login(v.username, v.password)
              message.success('登录成功')
              navigate('/admin')
            } catch {
              message.error('用户名或密码错误')
            }
          }}
        >
          <Form.Item name="username" label="用户名" rules={[{ required: true }]}>
            <Input autoComplete="username" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true }]}>
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block>
            登录
          </Button>
        </Form>
        <div style={{ marginTop: 12, textAlign: 'center' }}>
          <Link to="/">返回首页</Link>
        </div>
      </Card>
    </div>
  )
}
