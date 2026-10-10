import { Button, Card, Space, Typography, theme } from 'antd'
import { Link } from 'react-router-dom'
import { Shield } from 'lucide-react'
import Logo from '../components/Logo'
import { prefetchLan, prefetchPickup, prefetchRelay, prefetchSetup } from '../lib/prefetch'
import { useAuth } from '../stores/auth'

const { Title, Paragraph, Text } = Typography

export default function Home() {
  const { token } = theme.useToken()
  const username = useAuth((s) => s.username)
  const setupNeeded = useAuth((s) => s.setupNeeded)

  return (
    <div className="page home-page">
      <div className="hero">
        <div className="hero-brand">
          <div className="logo-mark">
            <Logo size={104} />
          </div>
          <Title level={1} className="brand-title" style={{ margin: '14px 0 4px', color: token.colorText }}>
            飞船
          </Title>
          <Paragraph className="tagline">让文件，轻松飞到另一台设备。</Paragraph>
        </div>

        <div className="home-actions">
          <Card className="action-card" hoverable onMouseEnter={prefetchLan} onFocus={prefetchLan}>
            <Title level={4} className="card-title">
              局域网快传
            </Title>
            <Text type="secondary" className="card-desc">
              本地设备直连，无需登录，文件不落服务器。
            </Text>
            <Link to="/lan">
              <Button type="primary" block>
                开始快传
              </Button>
            </Link>
          </Card>

          <Card className="action-card" hoverable onMouseEnter={prefetchRelay} onFocus={prefetchRelay}>
            <Title level={4} className="card-title">
              互联网中转
            </Title>
            <Text type="secondary" className="card-desc">
              管理员上传后生成取件码，跨网络分享文件。
            </Text>
            <Link to={username ? '/admin' : '/login'}>
              <Button type="primary" block ghost>
                {username ? '进入管理' : '管理员入口'}
              </Button>
            </Link>
          </Card>

          <Card className="action-card" hoverable onMouseEnter={prefetchPickup} onFocus={prefetchPickup}>
            <Title level={4} className="card-title">
              取件
            </Title>
            <Text type="secondary" className="card-desc">
              输入取件码，下载分享给你的文件。
            </Text>
            <Link to="/pickup">
              <Button block>去取件</Button>
            </Link>
          </Card>
        </div>

        <Space className="home-footer" size="middle">
          {setupNeeded === true ? (
            <Link to="/setup" onMouseEnter={prefetchSetup} onFocus={prefetchSetup}>
              <Button icon={<Shield size={16} />} shape="round">
                首次初始化管理员
              </Button>
            </Link>
          ) : username ? (
            <Text type="secondary">已登录：{username}</Text>
          ) : null}
          <Text type="secondary" className="hint">
            局域网模式依赖 WebRTC 直连；复杂网络可能失败。
          </Text>
        </Space>
      </div>
    </div>
  )
}
