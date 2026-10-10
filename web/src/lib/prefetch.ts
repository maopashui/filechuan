/** 悬停/聚焦时预取互联网中转相关 chunk，进入时更顺滑 */
export function prefetchRelay() {
  void import('../pages/Login')
  void import('../pages/Admin')
}

export function prefetchPickup() {
  void import('../pages/Pickup')
}

export function prefetchSetup() {
  void import('../pages/Setup')
}

export function prefetchLan() {
  void import('../pages/Lan')
}
