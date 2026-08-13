import type { FormEvent, ReactNode } from 'react'
import { motion } from 'framer-motion'
import { ArrowRight, Eye, EyeOff, Lock, User } from 'lucide-react'

type LoginPageShellProps = {
  children: ReactNode
}

type LoginCardProps = {
  username: string
  password: string
  showPassword: boolean
  loading: boolean
  onUsernameChange: (value: string) => void
  onPasswordChange: (value: string) => void
  onTogglePassword: () => void
  onSubmit: (event: FormEvent) => void
}

type LoginInputProps = {
  label: string
  value: string
  type: string
  autoComplete: string
  placeholder: string
  delay: number
  icon: ReactNode
  autoFocus?: boolean
  trailing?: ReactNode
  onChange: (value: string) => void
}

export function LoginPageShell({ children }: LoginPageShellProps) {
  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center overflow-hidden bg-[#07070c] px-4">
      <LoginBackground />
      {children}
      <LoginFooter />
    </div>
  )
}

/* 影院氛围：漂移光斑 + 细网格 + 暗角 */
function LoginBackground() {
  return (
    <div className="pointer-events-none absolute inset-0 z-0">
      <motion.div
        animate={{ x: [0, 40, -20, 0], y: [0, -30, 20, 0] }}
        transition={{ repeat: Infinity, duration: 26, ease: 'easeInOut' }}
        className="absolute -top-32 left-1/2 h-[560px] w-[720px] -translate-x-1/2 rounded-full blur-[140px]"
        style={{ background: 'radial-gradient(circle, rgba(124,58,237,0.28), transparent 65%)' }}
      />
      <motion.div
        animate={{ x: [0, -30, 25, 0], y: [0, 25, -15, 0] }}
        transition={{ repeat: Infinity, duration: 32, ease: 'easeInOut' }}
        className="absolute -bottom-40 -left-24 h-[480px] w-[560px] rounded-full blur-[130px]"
        style={{ background: 'radial-gradient(circle, rgba(6,182,212,0.14), transparent 65%)' }}
      />
      <motion.div
        animate={{ opacity: [0.5, 0.9, 0.5] }}
        transition={{ repeat: Infinity, duration: 12, ease: 'easeInOut' }}
        className="absolute -right-32 top-1/3 h-[420px] w-[420px] rounded-full blur-[120px]"
        style={{ background: 'radial-gradient(circle, rgba(217,70,239,0.1), transparent 65%)' }}
      />
      <div className="absolute inset-0 bg-[linear-gradient(rgba(255,255,255,0.025)_1px,transparent_1px),linear-gradient(90deg,rgba(255,255,255,0.025)_1px,transparent_1px)] [background-size:44px_44px] [mask-image:radial-gradient(ellipse_70%_60%_at_50%_40%,black,transparent)]" />
      <div className="absolute inset-0" style={{ background: 'radial-gradient(ellipse 90% 80% at 50% 50%, transparent 55%, rgba(0,0,0,0.5) 100%)' }} />
    </div>
  )
}

function LoginFooter() {
  return (
    <motion.p
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ delay: 0.9 }}
      className="absolute bottom-6 z-10 text-[11px] font-medium tracking-widest text-white/25"
    >
      MEDIASTATIONGO · 私人影院，由此开启
    </motion.p>
  )
}

export function LoginCard(props: LoginCardProps) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 32, scale: 0.97 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      transition={{ duration: 0.8, ease: [0.16, 1, 0.3, 1] }}
      className="relative z-10 w-full max-w-[420px]"
    >
      {/* 卡片背后的品牌光晕 */}
      <div className="absolute -inset-8 -z-10 rounded-[3rem] blur-3xl" style={{ background: 'radial-gradient(circle, rgba(124,58,237,0.16), transparent 70%)' }} />
      <div className="rounded-[1.75rem] border border-white/10 bg-white/[0.04] p-8 shadow-[0_32px_80px_rgba(0,0,0,0.5),inset_0_1px_0_rgba(255,255,255,0.08)] backdrop-blur-2xl sm:p-10">
        <LoginBrandHeader />
        <LoginForm {...props} />
      </div>
    </motion.div>
  )
}

function LoginBrandHeader() {
  return (
    <div className="flex flex-col items-center pb-9 text-center">
      <motion.div
        initial={{ scale: 0.7, opacity: 0 }}
        animate={{ scale: 1, opacity: 1 }}
        transition={{ delay: 0.15, type: 'spring', stiffness: 180, damping: 16 }}
        className="relative mb-5"
      >
        <div className="absolute -inset-3 rounded-[1.75rem] bg-brand-500/25 blur-xl" />
        <img
          src="/brand/mediastationgo-logo.svg"
          alt="MediaStationGo"
          className="relative h-16 w-16 rounded-2xl object-contain shadow-glow"
        />
      </motion.div>

      <motion.h1
        initial={{ opacity: 0, y: 10 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ delay: 0.25 }}
        className="font-display text-[1.7rem] font-extrabold tracking-tight text-white"
      >
        Media<span className="text-gradient-brand">Station</span>Go
      </motion.h1>

      <motion.p
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ delay: 0.35 }}
        className="mt-2.5 text-[11px] font-bold uppercase tracking-[0.3em] text-gold-400"
      >
        一站式媒体管理中心
      </motion.p>
    </div>
  )
}

function LoginForm({
  username,
  password,
  showPassword,
  loading,
  onUsernameChange,
  onPasswordChange,
  onTogglePassword,
  onSubmit,
}: LoginCardProps) {
  return (
    <form onSubmit={onSubmit} className="space-y-5">
      <LoginInput
        label="用户名"
        type="text"
        value={username}
        autoComplete="username"
        placeholder="请输入您的账号"
        delay={0.4}
        icon={<User size={16} />}
        autoFocus
        onChange={onUsernameChange}
      />
      <LoginInput
        label="安全密码"
        type={showPassword ? 'text' : 'password'}
        value={password}
        autoComplete="current-password"
        placeholder="••••••••"
        delay={0.48}
        icon={<Lock size={16} />}
        trailing={<PasswordVisibilityButton visible={showPassword} onToggle={onTogglePassword} />}
        onChange={onPasswordChange}
      />
      <LoginSubmitButton loading={loading} />
    </form>
  )
}

function LoginInput({
  label,
  value,
  type,
  autoComplete,
  placeholder,
  delay,
  icon,
  autoFocus,
  trailing,
  onChange,
}: LoginInputProps) {
  return (
    <motion.div initial={{ opacity: 0, x: -12 }} animate={{ opacity: 1, x: 0 }} transition={{ delay, duration: 0.5, ease: [0.21, 0.47, 0.32, 0.98] }}>
      <label className="mb-2 block text-[11px] font-bold uppercase tracking-[0.18em] text-white/40">{label}</label>
      <div className="relative">
        <span className="absolute left-4 top-1/2 -translate-y-1/2 text-white/35">{icon}</span>
        <input
          type={type}
          className="w-full rounded-xl border border-white/10 bg-white/[0.05] px-11 py-3.5 pr-12 text-sm text-white placeholder-white/30 outline-none transition-all duration-300 focus:border-brand-400/60 focus:bg-white/[0.08] focus:shadow-glow-sm"
          value={value}
          onChange={(event) => onChange(event.target.value)}
          autoComplete={autoComplete}
          autoFocus={autoFocus}
          required
          placeholder={placeholder}
        />
        {trailing}
      </div>
    </motion.div>
  )
}

function PasswordVisibilityButton({ visible, onToggle }: { visible: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      onClick={onToggle}
      className="absolute right-4 top-1/2 -translate-y-1/2 text-white/35 transition-colors hover:text-white/80"
      tabIndex={-1}
    >
      {visible ? <EyeOff size={16} /> : <Eye size={16} />}
    </button>
  )
}

function LoginSubmitButton({ loading }: { loading: boolean }) {
  return (
    <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.56 }} className="pt-3">
      <button
        type="submit"
        disabled={loading}
        className="btn-primary w-full py-4 text-[15px] tracking-wide"
      >
        {loading ? <LoginSpinnerLabel /> : <LoginReadyLabel />}
      </button>
    </motion.div>
  )
}

function LoginSpinnerLabel() {
  return (
    <span className="flex items-center gap-2">
      <svg className="h-4 w-4 animate-spin text-white" viewBox="0 0 24 24" fill="none">
        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
      </svg>
      正在开启影院…
    </span>
  )
}

function LoginReadyLabel() {
  return (
    <span className="flex items-center gap-2">
      开启观影之旅 <ArrowRight size={16} />
    </span>
  )
}
