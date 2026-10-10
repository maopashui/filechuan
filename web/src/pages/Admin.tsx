import { useCallback, useEffect, useState } from 'react'
import {
  Button,
  Card,
  Progress,
  Select,
  Space,
  Table,
  Typography,
  Upload,
  message,
  Popconfirm,
} from 'antd'
import { Link, useNavigate } from 'react-router-dom'
import dayjs from 'dayjs'
import {
  abortUpload,
  completeUpload,
  createUpload,
  deleteFile,
  getPublicConfig,
  listFiles,
  putPart,
  regeneratePickup,
  revokePickup,
  type FileItem,
} from '../api/client'
import { useAuth } from '../stores/auth'
import Logo from '../components/Logo'

function formatSize(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

export default function Admin() {
  const username = useAuth((s) => s.username)
  const logout = useAuth((s) => s.logout)
  const navigate = useNavigate()
  const [files, setFiles] = useState<FileItem[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [expiresDays, setExpiresDays] = useState(30)
  const [progress, setProgress] = useState<number | null>(null)
  const [uploadingName, setUploadingName] = useState('')
  const [lastCode, setLastCode] = useState<string | null>(null)
  const [maxSize, setMaxSize] = useState(2 * 1024 * 1024 * 1024)

  const refresh = useCallback(async () => {
    const data = await listFiles(page, 20)
    setFiles(data.items)
    setTotal(data.total)
  }, [page])

  useEffect(() => {
    if (!username) {
      navigate('/login')
      return
    }
    getPublicConfig().then((c) => setMaxSize(c.max_file_size)).catch(() => {})
    refresh().catch(() => message.error('加载文件列表失败'))
  }, [username, navigate, refresh])

  async function uploadFile(file: File) {
    if (file.size > maxSize) {
      message.error(`超过大小限制 ${formatSize(maxSize)}`)
      return
    }
    setUploadingName(file.name)
    setProgress(0)
    setLastCode(null)
    let uploadId = ''
    try {
      const session = await createUpload(file.name, file.size, expiresDays)
      uploadId = session.id
      const partSize = session.part_size
      const totalParts = session.total_parts
      for (let part = 1; part <= totalParts; part++) {
        const start = (part - 1) * partSize
        const blob = file.slice(start, start + partSize)
        await putPart(uploadId, part, blob)
        setProgress(Math.round((part / totalParts) * 100))
      }
      const rec = await completeUpload(uploadId)
      setLastCode(rec.pickup_code || null)
      message.success('上传成功')
      await refresh()
    } catch {
      if (uploadId) await abortUpload(uploadId).catch(() => {})
      message.error('上传失败')
    } finally {
      setProgress(null)
      setUploadingName('')
    }
  }

  return (
    <div className="page admin-page">
      <div className="page-header">
        <Space>
          <Logo size={40} />
          <div>
            <Typography.Title level={3} style={{ margin: 0 }}>
              互联网中转
            </Typography.Title>
            <Typography.Text type="secondary">管理员：{username}</Typography.Text>
          </div>
        </Space>
        <Space>
          <Link to="/">首页</Link>
          <Button
            onClick={async () => {
              await logout()
              navigate('/')
            }}
          >
            退出
          </Button>
        </Space>
      </div>

      <Card title="上传文件" style={{ marginBottom: 16 }}>
        <Space wrap>
          <span>有效期</span>
          <Select
            value={expiresDays}
            style={{ width: 120 }}
            onChange={setExpiresDays}
            options={[1, 3, 7, 15, 30].map((d) => ({ value: d, label: `${d} 天` }))}
          />
          <Upload
            beforeUpload={(f) => {
              void uploadFile(f)
              return false
            }}
            showUploadList={false}
            multiple
          >
            <Button type="primary">选择文件上传</Button>
          </Upload>
        </Space>
        {progress !== null && (
          <div style={{ marginTop: 16 }}>
            <Typography.Text>
              正在上传 {uploadingName}
            </Typography.Text>
            <Progress percent={progress} />
          </div>
        )}
        {lastCode && (
          <Typography.Paragraph copyable style={{ marginTop: 12 }}>
            取件码：{lastCode}
          </Typography.Paragraph>
        )}
      </Card>

      <Card title="文件列表">
        <Table
          rowKey="id"
          dataSource={files}
          pagination={{
            current: page,
            total,
            pageSize: 20,
            onChange: setPage,
          }}
          locale={{ emptyText: '还没有文件，上传后会出现在这里' }}
          columns={[
            { title: '文件名', dataIndex: 'original_name' },
            {
              title: '大小',
              dataIndex: 'size',
              render: (v: number) => formatSize(v),
              width: 100,
            },
            {
              title: '创建',
              dataIndex: 'created_at',
              render: (v: string) => dayjs(v).format('YYYY-MM-DD HH:mm'),
              width: 160,
            },
            {
              title: '到期',
              dataIndex: 'expires_at',
              render: (v: string) => dayjs(v).format('YYYY-MM-DD HH:mm'),
              width: 160,
            },
            { title: '下载', dataIndex: 'download_count', width: 70 },
            {
              title: '操作',
              width: 280,
              render: (_, row) => (
                <Space wrap>
                  {row.pickup_url && (
                    <Typography.Text copyable={{ text: `${window.location.origin}${row.pickup_url}` }}>
                      链接
                    </Typography.Text>
                  )}
                  <Button
                    size="small"
                    onClick={async () => {
                      try {
                        const r = await regeneratePickup(row.id)
                        message.success(`新取件码：${r.pickup_code}`)
                        setLastCode(r.pickup_code)
                        await refresh()
                      } catch {
                        message.error('生成失败')
                      }
                    }}
                  >
                    新取件码
                  </Button>
                  <Button
                    size="small"
                    onClick={async () => {
                      await revokePickup(row.id)
                      message.success('已撤销')
                      await refresh()
                    }}
                  >
                    撤销码
                  </Button>
                  <Popconfirm
                    title="确认删除？"
                    onConfirm={async () => {
                      await deleteFile(row.id)
                      message.success('已删除')
                      await refresh()
                    }}
                  >
                    <Button size="small" danger>
                      删除
                    </Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>
    </div>
  )
}
