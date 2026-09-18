# Vares

### Vares means Inheritor

a simple online distillator for llms.

currently supports SFT distillation. [more on other methods](https://youtu.be/YsMd4F3jfyg?si=yogfr5JcmPr_VURS)

---

## how it works

you serve your app that uses an llm, normally.

     ------ 
**self-hosted?** -> put `Echo` in front of your model's api.

**using a provider?** -> point your app at `Echo` instead of the provider directly.

     ------

either way, `Echo` forwards every request through untouched, and mirrors the input + output on the side.

`Apprentice` listens to `Echo`. it consumes the mirrored data, batches it, and trains the student for one step.

the student is checked and saved as it improves — you'll always have a checkpoint on disk or S3, ready to use.

the more you use the app, the stronger the student gets.