import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Construction } from 'lucide-react';

interface PlaceholderProps {
  title: string;
  description: string;
}

function PlaceholderCard({ title, description }: PlaceholderProps) {
  return (
    <div className="mx-auto max-w-xl rounded-2xl border border-slate-800 bg-slate-900/70 p-8 text-center my-8">
      <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-indigo-500/15 text-indigo-400 mb-4">
        <Construction className="h-6 w-6" />
      </div>
      <h1 className="text-xl font-bold text-white">{title}</h1>
      <p className="mt-2 text-sm text-slate-400">{description}</p>
      <Link
        to="/"
        className="mt-6 inline-flex items-center gap-2 rounded-xl border border-slate-700 bg-slate-800 px-4 py-2 text-sm font-medium text-slate-200 hover:bg-slate-700 transition-colors"
      >
        <ArrowLeft className="h-4 w-4" />
        <span>返回课程库</span>
      </Link>
    </div>
  );
}

export function CoursePlayerPlaceholder() {
  const { courseId } = useParams();
  return (
    <PlaceholderCard
      title="课程播放与目录页"
      description={`课程 ${courseId ?? ''} 的章节播放器与进度跟踪将在后续任务中完善。`}
    />
  );
}

export function CourseEditPlaceholder() {
  const { courseId } = useParams();
  return (
    <PlaceholderCard
      title="课程编辑与 AI 整理"
      description={`课程 ${courseId ?? ''} 的元数据编辑、章节重排与 AI 辅助整理将在后续任务中完善。`}
    />
  );
}

export function StoragePlaceholder() {
  return (
    <PlaceholderCard
      title="存储源与目录导入"
      description="存储源管理、媒体扫描与目录导入向导将在后续任务中完善。"
    />
  );
}

export function SettingsPlaceholder() {
  return (
    <PlaceholderCard
      title="系统与 AI 设置"
      description="服务器名称、扫描间隔、AI 接口配置及多设备会话管理将在后续任务中完善。"
    />
  );
}
