import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Input,
  Progress,
  Space,
  Typography,
  Upload,
  message,
  Modal,
  List,
} from 'antd'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { QRCodeSVG } from 'qrcode.react'
import { createRoom, getPublicConfig } from '../api/client'
import Logo from '../components/Logo'
import {
  buildIceServers,
  createReceiver,
  sanitizeDownloadName,
  sendFiles,
  type IceConfig,
} from '../lib/webrtc'
import { loadPeerName, savePeerName, suggestPeerName } from '../lib/peerName'

type Peer = { id: string; name: string }
type ConnState = 'idle' | 'signaling' | 'connecting' | 'connected' | 'failed' | 'closed'
type ReceivedFile = { id: string; name: string; size: number; url: string; file: File }

function formatBytes(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

async function saveReceivedFile(item: Pick<ReceivedFile, 'name' | 'url' | 'file'>) {
  const filename = sanitizeDownloadName(item.name || item.file.name)
  const file =
    item.file.name === filename
      ? item.file
      : new File([item.file], filename, { type: item.file.type || 'application/octet-stream' })

  // Mobile Safari often ignores <a download> and invents *.txt; prefer system share sheet.
  const nav = navigator as Navigator & {
    canShare?: (data: ShareData) => boolean
    share?: (data: ShareData) => Promise<void>
  }
  if (nav.share && nav.canShare?.({ files: [file] })) {
    try {
      await nav.share({ files: [file], title: filename })
      return
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') return
      /* fall through to anchor download */
    }
  }

  const a = document.createElement('a')
  a.href = item.url
  a.download = filename
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  a.remove()
}

export default function Lan() {
  const { roomCode } = useParams()
  const navigate = useNavigate()
  const [room, setRoom] = useState(roomCode?.toUpperCase() || '')
  const [peerName, setPeerName] = useState(() => loadPeerName())
  const suggestedName = useMemo(() => suggestPeerName(), [])
  const [peers, setPeers] = useState<Peer[]>([])
  const [selfId, setSelfId] = useState('')
  const [connState, setConnState] = useState<ConnState>('idle')
  const [statusText, setStatusText] = useState('未连接')
  const [progress, setProgress] = useState<number | null>(null)
  const [progressLabel, setProgressLabel] = useState('')
  const [ice, setIce] = useState<IceConfig>({ stunServers: ['stun:stun.l.google.com:19302'] })
  const [joinCode, setJoinCode] = useState('')
  const [receivedFiles, setReceivedFiles] = useState<ReceivedFile[]>([])

  const wsRef = useRef<WebSocket | null>(null)
  const pcRef = useRef<RTCPeerConnection | null>(null)
  const dcRef = useRef<RTCDataChannel | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const receiverRef = useRef<ReturnType<typeof createReceiver> | null>(null)
  const receivedFilesRef = useRef<ReceivedFile[]>([])

  const inviteUrl = useMemo(() => {
    if (!room) return ''
    return `${window.location.origin}/lan/${room}`
  }, [room])

  useEffect(() => {
    getPublicConfig()
      .then((c) =>
        setIce({
          stunServers: c.stun_servers,
          turn: c.turn.url ? c.turn : undefined,
        }),
      )
      .catch(() => {})
  }, [])

  useEffect(() => {
    if (roomCode) {
      const code = roomCode.toUpperCase()
      setRoom(code)
      void connectRoom(code)
    }
    return () => cleanup()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [roomCode])

  function cleanup() {
    abortRef.current?.abort()
    dcRef.current?.close()
    pcRef.current?.close()
    wsRef.current?.close()
    dcRef.current = null
    pcRef.current = null
    wsRef.current = null
  }

  function clearReceivedFiles() {
    for (const f of receivedFilesRef.current) {
      URL.revokeObjectURL(f.url)
    }
    receivedFilesRef.current = []
    setReceivedFiles([])
  }

  useEffect(() => {
    return () => {
      for (const f of receivedFilesRef.current) {
        URL.revokeObjectURL(f.url)
      }
    }
  }, [])

  async function handleCreate() {
    try {
      const r = await createRoom()
      navigate(`/lan/${r.room}`)
    } catch {
      message.error('创建房间失败（可能触发限流）')
    }
  }

  function connectRoom(code: string) {
    cleanup()
    setConnState('signaling')
    setStatusText('连接信令…')
    const displayName = (peerName.trim() || suggestedName).slice(0, 32)
    savePeerName(displayName)
    if (displayName !== peerName) setPeerName(displayName)

    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${window.location.host}/ws/signaling`)
    wsRef.current = ws

    ws.onopen = () => {
      ws.send(JSON.stringify({ type: 'join', room: code, peerId: displayName }))
    }
    ws.onclose = () => {
      setConnState((s) => (s === 'connected' ? 'closed' : s))
      setStatusText('信令已断开')
    }
    ws.onerror = () => {
      setConnState('failed')
      setStatusText('信令连接失败')
    }
    ws.onmessage = async (ev) => {
      const msg = JSON.parse(ev.data)
      if (msg.type === 'joined') {
        setSelfId(msg.peerId)
        setPeers(msg.peers || [])
        setStatusText('已加入房间，等待配对…')
        const others = (msg.peers || []).filter((p: Peer) => p.id !== msg.peerId)
        if (others.length > 0) {
          await startAsOfferer(others[0].id)
        }
      } else if (msg.type === 'peer_joined') {
        setPeers((prev) => {
          const exists = prev.some((p) => p.id === msg.from)
          if (exists) return prev
          return [...prev, ...(msg.peers || [{ id: msg.from, name: 'Peer' }])]
        })
      } else if (msg.type === 'peer_left') {
        setPeers((prev) => prev.filter((p) => p.id !== msg.from))
        setStatusText('对方已离开')
      } else if (msg.type === 'room_closed') {
        message.warning('房间已关闭')
        cleanup()
        setConnState('closed')
      } else if (msg.type === 'signal') {
        await onSignal(msg.from, msg.payload)
      } else if (msg.type === 'error') {
        const map: Record<string, string> = {
          room_not_found: '房间不存在或已失效，请重新创建',
          room_expired: '房间已超过最长保留时间（24 小时）',
          room_full: '房间人数已满',
        }
        message.error(map[msg.error] || msg.error)
        if (msg.error === 'room_not_found' || msg.error === 'room_expired') {
          setConnState('failed')
          setStatusText(map[msg.error] || msg.error)
        }
      }
    }
  }

  function sendSignal(to: string, payload: unknown) {
    wsRef.current?.send(JSON.stringify({ type: 'signal', to, payload }))
  }

  async function ensurePC(remoteId: string) {
    if (pcRef.current) return pcRef.current
    const pc = new RTCPeerConnection({ iceServers: buildIceServers(ice) })
    pcRef.current = pc
    pc.onicecandidate = (e) => {
      if (e.candidate) {
        sendSignal(remoteId, { kind: 'ice', candidate: e.candidate })
      }
    }
    pc.onconnectionstatechange = () => {
      const st = pc.connectionState
      if (st === 'connected') {
        setConnState('connected')
        setStatusText('WebRTC 已连接')
      } else if (st === 'failed') {
        setConnState('failed')
        setStatusText('直连失败。请确认双方在可互通网络，或配置 TURN 后重试。文件不会上传到服务器。')
      } else if (st === 'connecting') {
        setConnState('connecting')
        setStatusText('正在建立直连…')
      } else if (st === 'disconnected' || st === 'closed') {
        setConnState('closed')
        setStatusText('连接已关闭')
      }
    }
    pc.ondatachannel = (ev) => {
      setupChannel(ev.channel)
    }
    return pc
  }

  function setupChannel(dc: RTCDataChannel) {
    dcRef.current = dc
    dc.binaryType = 'arraybuffer'
    const receiver = createReceiver({
      onOffer: (transferId, files) => {
        Modal.confirm({
          title: '收到文件传输请求',
          content: files.map((f) => `${f.name} (${f.size} bytes)`).join('\n'),
          okText: '接受',
          cancelText: '拒绝',
          onOk: () => receiver.accept(dc, transferId),
          onCancel: () => receiver.reject(dc, transferId),
        })
      },
      onProgress: (p) => {
        setProgress(Math.round((p.overallSent / Math.max(p.overallTotal, 1)) * 100))
        setProgressLabel(`接收 ${p.fileName}`)
      },
      onFileReady: (file) => {
        const name = sanitizeDownloadName(file.name)
        const typed =
          file.type && file.name === name
            ? file
            : new File([file], name, {
                type: file.type || 'application/octet-stream',
                lastModified: file.lastModified,
              })
        const url = URL.createObjectURL(typed)
        const item: ReceivedFile = {
          id: `${Date.now()}-${name}-${typed.size}`,
          name,
          size: typed.size,
          url,
          file: typed,
        }
        receivedFilesRef.current = [...receivedFilesRef.current, item]
        setReceivedFiles(receivedFilesRef.current)
        void saveReceivedFile(item)
        message.success(`已接收 ${name}，可在下方再次保存`)
      },
      onDone: () => {
        setProgress(100)
        setProgressLabel('接收完成')
        message.success('传输完成，请点击下方「保存」下载文件')
      },
      onError: (m) => message.error(m),
      onCancel: () => {
        setProgress(null)
        message.warning('传输已取消')
      },
    })
    receiverRef.current = receiver
    dc.onmessage = (ev) => {
      if (typeof ev.data === 'string') receiver.handleMessage(ev.data)
      else receiver.handleMessage(ev.data as ArrayBuffer)
    }
    dc.onopen = () => {
      setConnState('connected')
      setStatusText('数据通道已就绪')
    }
  }

  async function startAsOfferer(remoteId: string) {
    const pc = await ensurePC(remoteId)
    const dc = pc.createDataChannel('file')
    setupChannel(dc)
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    setConnState('connecting')
    sendSignal(remoteId, { kind: 'sdp', sdp: offer })
  }

  async function onSignal(from: string, payload: { kind: string; sdp?: RTCSessionDescriptionInit; candidate?: RTCIceCandidateInit }) {
    if (payload.kind === 'sdp' && payload.sdp) {
      if (payload.sdp.type === 'offer') {
        const pc = await ensurePC(from)
        await pc.setRemoteDescription(payload.sdp)
        const answer = await pc.createAnswer()
        await pc.setLocalDescription(answer)
        sendSignal(from, { kind: 'sdp', sdp: answer })
        setConnState('connecting')
      } else if (payload.sdp.type === 'answer') {
        const pc = pcRef.current
        if (pc) await pc.setRemoteDescription(payload.sdp)
      }
    } else if (payload.kind === 'ice' && payload.candidate) {
      try {
        await pcRef.current?.addIceCandidate(payload.candidate)
      } catch {
        /* ignore late candidates */
      }
    }
  }

  async function onSend(files: File[]) {
    const dc = dcRef.current
    if (!dc || dc.readyState !== 'open') {
      message.error('尚未建立数据通道，无法发送')
      return
    }
    abortRef.current = new AbortController()
    setProgress(0)
    try {
      await sendFiles(
        dc,
        files,
        (p) => {
          setProgress(Math.round((p.overallSent / Math.max(p.overallTotal, 1)) * 100))
          setProgressLabel(`发送 ${p.fileName}`)
        },
        abortRef.current.signal,
      )
      message.success('发送完成')
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '发送失败')
    }
  }

  return (
    <div className="page lan-page">
      <div className="page-header">
        <Space>
          <Logo size={40} />
          <Typography.Title level={3} style={{ margin: 0 }}>
            局域网快传
          </Typography.Title>
        </Space>
        <Link to="/">首页</Link>
      </div>

      {!roomCode && (
        <Card>
          <Space direction="vertical" style={{ width: '100%' }} size="large">
            <div>
              <Typography.Paragraph>创建临时房间，用链接或房间码邀请另一台设备。</Typography.Paragraph>
              <Space wrap>
                <Input
                  placeholder={suggestedName}
                  value={peerName}
                  maxLength={32}
                  onChange={(e) => setPeerName(e.target.value)}
                  onBlur={() => {
                    const v = peerName.trim()
                    if (v) savePeerName(v)
                  }}
                  style={{ width: 220 }}
                  allowClear
                />
                <Button
                  type="link"
                  onClick={() => {
                    setPeerName(suggestedName)
                    savePeerName(suggestedName)
                  }}
                >
                  用设备名
                </Button>
                <Button type="primary" onClick={() => void handleCreate()}>
                  创建房间
                </Button>
              </Space>
              <Typography.Paragraph type="secondary" style={{ marginTop: 8, marginBottom: 0 }}>
                默认按浏览器与设备识别（如 {suggestedName}），可自行修改。
              </Typography.Paragraph>
            </div>
            <div>
              <Typography.Text>或输入房间码加入</Typography.Text>
              <Space style={{ marginTop: 8 }}>
                <Input
                  value={joinCode}
                  onChange={(e) => setJoinCode(e.target.value.toUpperCase())}
                  placeholder="房间码"
                  style={{ width: 160 }}
                />
                <Button
                  onClick={() => {
                    if (joinCode.trim()) navigate(`/lan/${joinCode.trim()}`)
                  }}
                >
                  加入
                </Button>
              </Space>
            </div>
          </Space>
        </Card>
      )}

      {roomCode && (
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <Card title={`房间 ${room}`}>
            <Space wrap align="start">
              <div>
                <Typography.Paragraph copyable={{ text: inviteUrl }}>邀请链接已就绪</Typography.Paragraph>
                <Typography.Paragraph copyable>{inviteUrl}</Typography.Paragraph>
                <Typography.Text type="secondary">房间码：{room}</Typography.Text>
              </div>
              {inviteUrl && <QRCodeSVG value={inviteUrl} size={128} />}
            </Space>
            <Alert style={{ marginTop: 12 }} type="info" showIcon message={statusText} />
            <List
              style={{ marginTop: 12 }}
              header="参与者"
              dataSource={peers}
              locale={{ emptyText: '等待加入…' }}
              renderItem={(p) => (
                <List.Item>
                  {p.name || p.id}
                  {p.id === selfId ? '（我）' : ''}
                </List.Item>
              )}
            />
            <Space style={{ marginTop: 12 }} wrap>
              <Button
                onClick={() => {
                  wsRef.current?.send(JSON.stringify({ type: 'close_room' }))
                  cleanup()
                  navigate('/lan')
                }}
              >
                关闭房间
              </Button>
              <Button
                danger
                onClick={() => {
                  cleanup()
                  navigate('/lan')
                }}
              >
                退出
              </Button>
            </Space>
          </Card>

          <Card title="传输文件">
            {connState === 'failed' && (
              <Alert
                type="error"
                showIcon
                style={{ marginBottom: 12 }}
                message="直连失败"
                description="不会将文件上传到服务器。可重试、更换网络，或由管理员配置 TURN。"
              />
            )}
            <Upload
              multiple
              beforeUpload={() => false}
              onChange={(info) => {
                const files = info.fileList.map((f) => f.originFileObj).filter(Boolean) as File[]
                if (files.length) void onSend(files)
              }}
              showUploadList={false}
              disabled={connState !== 'connected'}
            >
              <Button type="primary" disabled={connState !== 'connected'}>
                选择文件发送
              </Button>
            </Upload>
            <Button
              style={{ marginLeft: 8 }}
              disabled={progress === null}
              onClick={() => abortRef.current?.abort()}
            >
              取消传输
            </Button>
            {progress !== null && (
              <div style={{ marginTop: 12 }}>
                <Typography.Text>{progressLabel}</Typography.Text>
                <Progress percent={progress} />
              </div>
            )}
            {connState === 'connected' && (
              <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0 }}>
                对方发来文件时会弹出确认；接收完成后可在下方「已接收文件」里点保存。
              </Typography.Paragraph>
            )}
          </Card>

          <Card
            title="已接收文件"
            extra={
              receivedFiles.length > 0 ? (
                <Button type="link" size="small" onClick={clearReceivedFiles}>
                  清空
                </Button>
              ) : null
            }
          >
            <List
              dataSource={receivedFiles}
              locale={{ emptyText: '还没有收到文件。对方发送并经你接受后，会出现在这里。' }}
              renderItem={(f) => (
                <List.Item
                  actions={[
                    <Button
                      key="save"
                      type="primary"
                      size="small"
                      onClick={() => {
                        void saveReceivedFile(f).then(() => {
                          message.success(`已保存 ${f.name}`)
                        })
                      }}
                    >
                      保存
                    </Button>,
                  ]}
                >
                  <List.Item.Meta title={f.name} description={formatBytes(f.size)} />
                </List.Item>
              )}
            />
          </Card>
        </Space>
      )}
    </div>
  )
}
