interface LogoProps {
  size?: number;
  containerClassName?: string;
  showBackground?: boolean;
}

export default function Logo({
  size = 60,
  containerClassName = "mb-8",
  showBackground = true,
}: LogoProps = {}) {
  return (
    <div className={`flex justify-center select-none ${containerClassName}`}>
      <img
        src="/hardllama.png"
        alt="HardLlama"
        width={size}
        height={size}
        draggable={false}
        className={showBackground ? "rounded-full bg-white" : undefined}
      />
    </div>
  );
}
