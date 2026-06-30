"use client";

import { useParams } from "next/navigation";
import { CourtroomScene } from "@/components/courtroom/CourtroomScene";

export default function CourtPage() {
  const params = useParams();
  const sessionId = params.id as string;

  return <CourtroomScene sessionId={sessionId} />;
}
