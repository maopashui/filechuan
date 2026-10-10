type Props = {
  size?: number
  variant?: 'light' | 'dark'
  className?: string
}

/** Q-version cute spaceship brand mark */
export function Logo({ size = 48, variant = 'light', className }: Props) {
  const body = variant === 'dark' ? '#7DD3FC' : '#38BDF8'
  const bodyDeep = variant === 'dark' ? '#38BDF8' : '#0EA5E9'
  const cabin = variant === 'dark' ? '#E0F2FE' : '#F0F9FF'
  const accent = '#FB923C'
  const eye = variant === 'dark' ? '#0F172A' : '#1E293B'
  const blush = '#FDA4AF'
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 64 64"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
      role="img"
      aria-label="飞船"
    >
      {/* soft glow */}
      <ellipse cx="32" cy="52" rx="14" ry="3.5" fill={bodyDeep} opacity="0.18" />
      {/* stars */}
      <circle cx="52" cy="12" r="2.2" fill={body} opacity="0.75" />
      <circle cx="10" cy="18" r="1.4" fill={accent} opacity="0.85" />
      <circle cx="48" cy="22" r="1.2" fill={blush} opacity="0.7" />
      {/* body */}
      <ellipse cx="32" cy="34" rx="19" ry="15" fill={body} />
      <ellipse cx="32" cy="36" rx="16" ry="11" fill={bodyDeep} opacity="0.22" />
      {/* cabin / face */}
      <ellipse cx="32" cy="29.5" rx="11.5" ry="9.5" fill={cabin} />
      {/* blush */}
      <ellipse cx="24.5" cy="31.5" rx="2.4" ry="1.5" fill={blush} opacity="0.75" />
      <ellipse cx="39.5" cy="31.5" rx="2.4" ry="1.5" fill={blush} opacity="0.75" />
      {/* big eyes */}
      <ellipse cx="27.5" cy="28.5" rx="3.1" ry="3.4" fill={eye} />
      <ellipse cx="36.5" cy="28.5" rx="3.1" ry="3.4" fill={eye} />
      <circle cx="28.4" cy="27.4" r="1.15" fill="#fff" />
      <circle cx="37.4" cy="27.4" r="1.15" fill="#fff" />
      <circle cx="26.6" cy="29.6" r="0.55" fill="#fff" opacity="0.7" />
      <circle cx="35.6" cy="29.6" r="0.55" fill="#fff" opacity="0.7" />
      {/* smile */}
      <path
        d="M28 33.2c1.6 2.2 6.4 2.2 8 0"
        stroke={eye}
        strokeWidth="1.6"
        strokeLinecap="round"
      />
      {/* fins */}
      <path d="M17 41c-4.5 2.2-8 6.5-9 11 6.5-1.2 12-3.4 15.5-7" fill={accent} />
      <path d="M47 41c4.5 2.2 8 6.5 9 11-6.5-1.2-12-3.4-15.5-7" fill={accent} />
      {/* thruster puff */}
      <path
        d="M27.5 47c1.8 4.5 7.2 4.5 9 0"
        stroke={bodyDeep}
        strokeWidth="3.2"
        strokeLinecap="round"
        opacity="0.45"
      />
      <circle cx="29" cy="52" r="1.6" fill={accent} opacity="0.55" />
      <circle cx="35" cy="54" r="1.1" fill={accent} opacity="0.4" />
    </svg>
  )
}

export default Logo
